// Package service contains the application use cases that the transport layer
// invokes. It composes storage, the processing queue, and id generation,
// keeping HTTP handlers thin and the business workflow testable in isolation.
package service

import (
	"context"
	"time"

	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/id"
	"github.com/example/notification-service/internal/processing"
	"github.com/example/notification-service/internal/storage"
)

// NotificationService implements the submit / get / list use cases.
type NotificationService struct {
	repo      storage.Repository
	queue     *processing.Queue
	idGen     func() string
	now       func() time.Time
	supported map[domain.Channel]struct{}
}

// Option configures a NotificationService.
type Option func(*NotificationService)

// WithIDGenerator overrides the default id generator (useful in tests).
func WithIDGenerator(fn func() string) Option {
	return func(s *NotificationService) { s.idGen = fn }
}

// WithClock overrides the clock (useful in tests).
func WithClock(fn func() time.Time) Option {
	return func(s *NotificationService) { s.now = fn }
}

// New builds a service. supported is the set of channels the service accepts.
func New(repo storage.Repository, queue *processing.Queue, supported []domain.Channel, opts ...Option) *NotificationService {
	s := &NotificationService{
		repo:      repo,
		queue:     queue,
		idGen:     id.New,
		now:       func() time.Time { return time.Now().UTC() },
		supported: make(map[domain.Channel]struct{}, len(supported)),
	}
	for _, c := range supported {
		s.supported[c] = struct{}{}
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create validates the request shape, persists the notification in accepted
// state, and enqueues it for processing. Enqueue backpressure is surfaced as
// domain.ErrQueueFull.
func (s *NotificationService) Create(ctx context.Context, channel domain.Channel, recipients []domain.Recipient, message domain.Message) (*domain.Notification, error) {
	if _, ok := s.supported[channel]; !ok {
		return nil, domain.ErrChannelUnsupported
	}
	if len(recipients) == 0 {
		return nil, domain.NewValidationError("missing_recipients", "recipients", "at least one recipient is required")
	}

	now := s.now()
	n := &domain.Notification{
		ID:         domain.ID(s.idGen()),
		Channel:    channel,
		Recipients: recipients,
		Message:    message,
		Status:     domain.StatusAccepted,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	for _, r := range recipients {
		n.Results = append(n.Results, domain.RecipientResult{
			RecipientID: r.ID,
			Status:      domain.StatusAccepted,
			UpdatedAt:   now,
		})
	}

	if err := s.repo.Create(ctx, n); err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(n.ID); err != nil {
		// Persist the queue-full state so a later GET reflects it.
		_ = s.repo.SetStatus(ctx, n.ID, domain.StatusFailed)
		return nil, err
	}
	return n, nil
}

// Get returns a notification by id.
func (s *NotificationService) Get(ctx context.Context, id domain.ID) (*domain.Notification, error) {
	return s.repo.Get(ctx, id)
}

// List returns a page of notifications and the total count.
func (s *NotificationService) List(ctx context.Context, limit, offset int) ([]*domain.Notification, int, error) {
	return s.repo.List(ctx, limit, offset)
}
