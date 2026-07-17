// Package service is the orchestration core. It ties together the store, the
// validation pipeline, the worker pool and the delivery layer, and exposes the
// use-cases the API calls: submit, get and list notifications.
//
// The submit path is intentionally asynchronous. It expands a request into one
// notification per recipient, persists each as "pending", hands them to the
// worker pool and returns immediately. Processing (validate then deliver)
// happens on the pool. This keeps request latency low and independent of slow
// providers, and makes bursty traffic absorbable by the queue.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/store"
)

// validator runs the pre-delivery checks. Declared as an interface so the
// service does not depend on the concrete validation.Pipeline type.
type validator interface {
	Validate(ctx context.Context, n *domain.Notification) error
}

// deliverer performs reliable best-effort delivery of a single notification.
type deliverer interface {
	Deliver(ctx context.Context, n *domain.Notification) DeliveryResult
}

// DeliveryResult mirrors delivery.Result but is redeclared here so the service
// package does not import the delivery package (keeping the dependency arrows
// pointing inward). The wiring in cmd adapts between the two.
type DeliveryResult struct {
	Attempts int
	Err      error
}

// enqueuer hands a notification ID to background processing.
type enqueuer interface {
	Submit(ctx context.Context, notificationID string) error
}

// Service implements the notification use-cases.
type Service struct {
	store    store.Store
	validate validator
	deliver  deliverer
	queue    enqueuer
	channels map[domain.Channel]bool // channels that have a registered provider
	clock    func() time.Time
	log      *slog.Logger
}

// Deps bundles the service's collaborators. Passing them explicitly keeps wiring
// visible and testable (each can be a fake).
type Deps struct {
	Store          store.Store
	Validator      validator
	Deliverer      deliverer
	Queue          enqueuer
	SupportedChans []domain.Channel
	Clock          func() time.Time
	Logger         *slog.Logger
}

// New constructs a Service.
func New(d Deps) *Service {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	channels := make(map[domain.Channel]bool, len(d.SupportedChans))
	for _, c := range d.SupportedChans {
		channels[c] = true
	}
	return &Service{
		store:    d.Store,
		validate: d.Validator,
		deliver:  d.Deliverer,
		queue:    d.Queue,
		channels: channels,
		clock:    clock,
		log:      log,
	}
}

// SubmitRequest is the input to Submit: a channel, one or more recipients and a
// message shared by all of them.
type SubmitRequest struct {
	Channel    domain.Channel
	Recipients []string
	Message    domain.Message
}

// Submit validates the request shape, fans it out into one notification per
// recipient, persists each and enqueues it for processing. It returns the
// created notifications (in "pending" state, or "failed" if they could not be
// enqueued). Per-recipient content and policy checks run later, on the worker.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) ([]*domain.Notification, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	requestID := newID()
	now := s.clock()
	created := make([]*domain.Notification, 0, len(req.Recipients))

	for _, recipient := range req.Recipients {
		n := &domain.Notification{
			ID:        newID(),
			RequestID: requestID,
			Channel:   req.Channel,
			Recipient: recipient,
			Message:   req.Message,
			Status:    domain.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.store.Create(ctx, n); err != nil {
			// A create failure is an infrastructure problem; surface it rather
			// than silently dropping the recipient.
			return nil, fmt.Errorf("persisting notification for %q: %w", recipient, err)
		}

		// Enqueue for background processing. If the pool is saturated or closing,
		// mark the record failed so the client can observe it instead of it
		// hanging in "pending" forever.
		if err := s.queue.Submit(ctx, n.ID); err != nil {
			s.log.Warn("could not enqueue notification",
				slog.String("notification_id", n.ID),
				slog.String("error", err.Error()),
			)
			s.markFailed(ctx, n, fmt.Sprintf("could not enqueue: %v", err))
		}
		created = append(created, n)
	}

	s.log.Info("notifications submitted",
		slog.String("request_id", requestID),
		slog.String("channel", string(req.Channel)),
		slog.Int("count", len(created)),
	)
	return created, nil
}

// Process is the worker Handler: it runs validation then delivery for a single
// notification, moving it through its lifecycle states. It is invoked by the
// worker pool, never directly by the API.
func (s *Service) Process(ctx context.Context, notificationID string) {
	n, err := s.store.Get(ctx, notificationID)
	if err != nil {
		s.log.Error("worker could not load notification",
			slog.String("notification_id", notificationID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Validation phase — independent checks run concurrently with fail-fast.
	s.transition(ctx, n, domain.StatusValidating, "")
	if err := s.validate.Validate(ctx, n); err != nil {
		s.markFailed(ctx, n, err.Error())
		s.log.Info("notification rejected by validation",
			slog.String("notification_id", n.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Delivery phase — reliable best-effort send with retries/circuit breaking.
	s.transition(ctx, n, domain.StatusDelivering, "")
	res := s.deliver.Deliver(ctx, n)
	n.Attempts = res.Attempts
	if res.Err != nil {
		s.markFailed(ctx, n, res.Err.Error())
		s.log.Warn("notification delivery failed",
			slog.String("notification_id", n.ID),
			slog.Int("attempts", res.Attempts),
			slog.String("error", res.Err.Error()),
		)
		return
	}

	s.transition(ctx, n, domain.StatusDelivered, "")
	s.log.Info("notification delivered",
		slog.String("notification_id", n.ID),
		slog.Int("attempts", res.Attempts),
	)
}

// Get returns a single notification by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.Notification, error) {
	return s.store.Get(ctx, id)
}

// List returns notifications matching the filter, newest first.
func (s *Service) List(ctx context.Context, f store.ListFilter) ([]*domain.Notification, int, error) {
	return s.store.List(ctx, f)
}

// --- helpers -----------------------------------------------------------------

// validateRequest performs cheap, structural request-level validation before any
// records are created. Per-recipient semantic validation happens later.
func (s *Service) validateRequest(req SubmitRequest) error {
	if !req.Channel.Valid() {
		return fmt.Errorf("%w: unknown channel %q", domain.ErrValidation, req.Channel)
	}
	if !s.channels[req.Channel] {
		return fmt.Errorf("%w: no provider available for channel %q", domain.ErrValidation, req.Channel)
	}
	if len(req.Recipients) == 0 {
		return fmt.Errorf("%w: at least one recipient is required", domain.ErrValidation)
	}
	if req.Message.Body == "" {
		return fmt.Errorf("%w: message body is required", domain.ErrValidation)
	}
	return nil
}

// transition updates a notification's status (and optional error) and persists
// it, stamping UpdatedAt. Persistence errors are logged but not propagated: the
// worker has no caller to return them to, and the in-memory copy stays correct
// for the rest of processing.
func (s *Service) transition(ctx context.Context, n *domain.Notification, status domain.Status, lastErr string) {
	n.Status = status
	n.LastError = lastErr
	n.UpdatedAt = s.clock()
	if err := s.store.Update(ctx, n); err != nil {
		s.log.Error("could not persist status transition",
			slog.String("notification_id", n.ID),
			slog.String("status", string(status)),
			slog.String("error", err.Error()),
		)
	}
}

func (s *Service) markFailed(ctx context.Context, n *domain.Notification, reason string) {
	s.transition(ctx, n, domain.StatusFailed, reason)
}

// ErrInvalidRequest is a convenience for callers that want a typed check; it is
// equivalent to errors.Is(err, domain.ErrValidation).
func IsValidationError(err error) bool { return errors.Is(err, domain.ErrValidation) }
