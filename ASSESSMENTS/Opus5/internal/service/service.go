// Package service is the application layer: it turns a client request into
// persisted state plus queued work, and answers queries about it.
//
// It is transport-agnostic on purpose. The HTTP handlers translate wire
// formats and status codes; every rule about *what* the service does lives
// here, so a second transport (gRPC, a queue consumer) would reuse it whole.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/platform/id"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/store"
)

// ErrOverloaded means the service has no capacity for this request right now.
// It is a load signal, not a client error: the same request will succeed once
// the backlog drains.
var ErrOverloaded = errors.New("service is at capacity")

// Limits bound what a single request may ask for.
type Limits struct {
	// MaxRecipients caps the fan-out of one request. It exists so a single
	// caller cannot claim the entire queue, and so the service can give a
	// clear answer instead of a mysterious 503.
	MaxRecipients int
}

// DefaultLimits returns the default request limits.
func DefaultLimits() Limits {
	return Limits{MaxRecipients: 1_000}
}

// SubmitCommand is a request to send one message to one or more recipients.
type SubmitCommand struct {
	Channel        domain.Channel
	Recipients     []domain.Recipient
	Message        domain.Message
	Metadata       map[string]string
	IdempotencyKey string
}

// Service orchestrates submission and queries.
type Service struct {
	store    store.Store
	queue    *queue.Queue
	registry *provider.Registry
	limits   Limits
	logger   *slog.Logger

	now         func() time.Time
	newID       func(prefix string) string
	maxPageSize int
}

// Option customises a Service.
type Option func(*Service)

// WithClock replaces the time source.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithIDGenerator replaces the identifier source, so tests can assert on
// stable IDs.
func WithIDGenerator(gen func(prefix string) string) Option {
	return func(s *Service) { s.newID = gen }
}

// New builds a Service.
func New(st store.Store, q *queue.Queue, registry *provider.Registry, limits Limits, logger *slog.Logger, opts ...Option) *Service {
	s := &Service{
		store:       st,
		queue:       q,
		registry:    registry,
		limits:      limits,
		logger:      logger,
		now:         time.Now,
		newID:       id.New,
		maxPageSize: 100,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Channels returns the channels this deployment can deliver to.
func (s *Service) Channels() []domain.Channel { return s.registry.Channels() }

// Submit validates a request, persists it and queues one job per recipient.
//
// It returns as soon as the work is durably recorded and admitted, rather
// than waiting for delivery: providers are slow and unreliable, and holding a
// client connection open for them would make the API as unreliable as they
// are. Clients poll GET /v1/notifications/{id} for the outcome.
func (s *Service) Submit(ctx context.Context, cmd SubmitCommand) (*domain.Notification, error) {
	if err := s.validateCommand(cmd); err != nil {
		return nil, err
	}

	// Idempotency: a client retrying after a timeout must not send twice.
	if cmd.IdempotencyKey != "" {
		existing, err := s.store.FindByIdempotencyKey(ctx, cmd.IdempotencyKey)
		switch {
		case err == nil:
			s.logger.InfoContext(ctx, "returning existing notification for idempotency key",
				slog.String("notification_id", existing.ID))
			return existing, nil
		case !errors.Is(err, domain.ErrNotFound):
			return nil, fmt.Errorf("look up idempotency key: %w", err)
		}
	}

	notification := s.build(cmd)

	// Reserve capacity before persisting: accepting a request we cannot
	// process would leave deliveries stuck in "pending" forever. All or
	// nothing, so a request is never partially queued.
	if err := s.queue.Reserve(len(notification.Deliveries)); err != nil {
		switch {
		case errors.Is(err, queue.ErrTooLarge):
			return nil, domain.Invalid("recipients",
				"%d recipients exceeds the queue capacity of %d", len(notification.Deliveries), s.queue.Capacity())
		case errors.Is(err, queue.ErrClosed):
			return nil, fmt.Errorf("%w: service is shutting down", ErrOverloaded)
		default:
			return nil, fmt.Errorf("%w: %w", ErrOverloaded, err)
		}
	}

	if err := s.store.Create(ctx, notification); err != nil {
		s.queue.Release(len(notification.Deliveries))
		return nil, fmt.Errorf("persist notification: %w", err)
	}

	published := 0
	for _, d := range notification.Deliveries {
		job := queue.Job{
			NotificationID: notification.ID,
			DeliveryID:     d.ID,
			Channel:        notification.Channel,
			Recipient:      d.Recipient,
			Message:        notification.Message,
			Metadata:       notification.Metadata,
			EnqueuedAt:     s.now(),
		}
		if err := s.queue.Publish(job); err != nil {
			// Only reachable if the queue closed between Reserve and here.
			// The notification is already stored, so record the truth on the
			// deliveries that will never run rather than losing them.
			s.abandonRemaining(ctx, notification, published, err)
			break
		}
		published++
	}

	s.logger.InfoContext(ctx, "notification accepted",
		slog.String("notification_id", notification.ID),
		slog.String("channel", notification.Channel.String()),
		slog.Int("recipients", len(notification.Deliveries)))

	return notification.Clone(), nil
}

// Get returns a notification by ID.
func (s *Service) Get(ctx context.Context, notificationID string) (*domain.Notification, error) {
	if strings.TrimSpace(notificationID) == "" {
		return nil, domain.Invalid("id", "a notification id is required")
	}
	return s.store.Get(ctx, notificationID)
}

// List returns a page of notifications, newest first.
func (s *Service) List(ctx context.Context, f store.Filter) (store.Page, error) {
	if f.Channel != "" && !s.registry.Supports(f.Channel) {
		return store.Page{}, domain.Invalid("channel",
			"channel %q is not supported; supported channels are %s", f.Channel, s.channelList())
	}
	if f.Status != "" && !validStatus(f.Status) {
		return store.Page{}, domain.Invalid("status",
			"status %q is not a valid status", f.Status)
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	f.Limit = min(f.Limit, s.maxPageSize)
	if f.Offset < 0 {
		return store.Page{}, domain.Invalid("offset", "offset must not be negative")
	}
	return s.store.List(ctx, f)
}

// validateCommand checks the request itself.
//
// It stops at the first problem: the requirement is explicit that a failed
// validation must not lead to further work, and a caller cannot act on a
// second error until the first is fixed anyway.
func (s *Service) validateCommand(cmd SubmitCommand) error {
	if cmd.Channel == "" {
		return domain.Invalid("channel", "a channel is required; supported channels are %s", s.channelList())
	}
	if !s.registry.Supports(cmd.Channel) {
		return domain.Invalid("channel",
			"channel %q is not supported; supported channels are %s", cmd.Channel, s.channelList())
	}
	if len(cmd.Recipients) == 0 {
		return domain.Invalid("recipients", "at least one recipient is required")
	}
	if len(cmd.Recipients) > s.limits.MaxRecipients {
		return domain.Invalid("recipients",
			"a request may target at most %d recipients, got %d", s.limits.MaxRecipients, len(cmd.Recipients))
	}
	if strings.TrimSpace(cmd.Message.Body) == "" {
		return domain.Invalid("message.body", "a message body is required")
	}

	// Addresses are checked syntactically here so an obviously wrong request
	// is refused synchronously, with a 4xx the caller can fix. The deeper,
	// stateful checks (preferences, rate limits) run per delivery in the
	// worker, where they belong.
	p, err := s.registry.Get(cmd.Channel)
	if err != nil {
		return domain.Invalid("channel", "channel %q is not supported", cmd.Channel)
	}
	seen := make(map[string]struct{}, len(cmd.Recipients))
	for i, r := range cmd.Recipients {
		if strings.TrimSpace(r.Address) == "" {
			return domain.Invalid(fmt.Sprintf("recipients[%d].address", i), "an address is required")
		}
		if err := p.ValidateAddress(r.Address); err != nil {
			return domain.Invalid(fmt.Sprintf("recipients[%d].address", i), "%s", err)
		}
		if _, dup := seen[r.Key()]; dup {
			return domain.Invalid(fmt.Sprintf("recipients[%d]", i),
				"duplicate recipient %q in the same request", r.Key())
		}
		seen[r.Key()] = struct{}{}
	}
	return nil
}

// build creates the aggregate with one pending delivery per recipient.
func (s *Service) build(cmd SubmitCommand) *domain.Notification {
	now := s.now()
	n := &domain.Notification{
		ID:             s.newID(id.PrefixNotification),
		Channel:        cmd.Channel,
		Message:        cmd.Message,
		Metadata:       cmd.Metadata,
		IdempotencyKey: cmd.IdempotencyKey,
		Deliveries:     make([]*domain.Delivery, 0, len(cmd.Recipients)),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	for _, r := range cmd.Recipients {
		n.Deliveries = append(n.Deliveries, &domain.Delivery{
			ID:             s.newID(id.PrefixDelivery),
			NotificationID: n.ID,
			Recipient:      r,
			Status:         domain.DeliveryPending,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	return n
}

// abandonRemaining marks the deliveries that could not be queued.
func (s *Service) abandonRemaining(ctx context.Context, n *domain.Notification, from int, cause error) {
	s.queue.Release(len(n.Deliveries) - from)
	now := s.now()
	for _, d := range n.Deliveries[from:] {
		d.Status = domain.DeliveryFailed
		d.FailureCode = "not_queued"
		d.FailureReason = cause.Error()
		d.CompletedAt = &now

		deliveryID := d.ID
		err := s.store.UpdateDelivery(ctx, n.ID, deliveryID, func(stored *domain.Delivery) {
			stored.Status = domain.DeliveryFailed
			stored.FailureCode = "not_queued"
			stored.FailureReason = cause.Error()
			stored.CompletedAt = &now
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "could not mark unqueued delivery as failed",
				slog.String("notification_id", n.ID),
				slog.String("delivery_id", deliveryID),
				slog.String("error", err.Error()))
		}
	}
	s.logger.ErrorContext(ctx, "notification was only partially queued",
		slog.String("notification_id", n.ID),
		slog.Int("queued", from),
		slog.Int("total", len(n.Deliveries)),
		slog.String("error", cause.Error()))
}

func (s *Service) channelList() string {
	channels := s.registry.Channels()
	names := make([]string, len(channels))
	for i, c := range channels {
		names[i] = c.String()
	}
	if len(names) == 0 {
		return "(none configured)"
	}
	return strings.Join(names, ", ")
}

func validStatus(s domain.Status) bool {
	switch s {
	case domain.StatusPending, domain.StatusProcessing, domain.StatusDelivered,
		domain.StatusPartiallyDelivered, domain.StatusFailed:
		return true
	default:
		return false
	}
}
