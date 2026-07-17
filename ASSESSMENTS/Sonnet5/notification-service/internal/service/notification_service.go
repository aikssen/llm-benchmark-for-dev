// Package service contains the application's use cases, orchestrating
// validation, persistence and asynchronous delivery. It is the only layer
// that talks to all of validation, store and the delivery queue, keeping
// the API layer a thin HTTP adapter and the lower layers unaware of each
// other.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/store"
	"github.com/aikssen/notification-service/internal/validation"
)

// Enqueuer schedules a persisted notification for asynchronous delivery.
// It is satisfied by *delivery.WorkerPool; declaring it here (rather than
// importing the delivery package) keeps this package's dependency surface
// minimal and easy to fake in tests.
type Enqueuer interface {
	Enqueue(id string) error
}

// NotificationService implements the notification use cases: submit,
// get status, and list.
type NotificationService struct {
	repo     store.Repository
	pipeline *validation.Pipeline
	queue    Enqueuer
	logger   *slog.Logger
}

// New builds a NotificationService.
func New(repo store.Repository, pipeline *validation.Pipeline, queue Enqueuer, logger *slog.Logger) *NotificationService {
	return &NotificationService{repo: repo, pipeline: pipeline, queue: queue, logger: logger}
}

// BatchResult is the outcome of submitting a batch request: one
// Notification per recipient, each with its own initial status.
// Recipient-level validation or delivery is guaranteed independent per
// notification, so a batch always contains a mix outcome — good
// recipients are never blocked by bad ones in the same request.
type BatchResult struct {
	BatchID       string
	Notifications []*domain.Notification
}

// Submit validates and persists a batch request, then schedules every
// recipient that passed validation for asynchronous delivery.
//
// Request-level validation (e.g. message content) runs once and, if it
// fails, the whole batch is rejected before any Notification is created —
// per the requirement that a failed validation must not waste resources
// on further work. Recipient-level validation then runs independently per
// recipient: one recipient failing (bad address, opted out, rate
// limited) does not affect its siblings.
func (s *NotificationService) Submit(ctx context.Context, req domain.BatchRequest) (*BatchResult, error) {
	if len(req.Recipients) == 0 {
		return nil, fmt.Errorf("%w: at least one recipient is required", domain.ErrValidation)
	}
	if err := s.pipeline.ValidateRequest(ctx, req); err != nil {
		return nil, err
	}

	batchID := uuid.NewString()
	notifications := make([]*domain.Notification, 0, len(req.Recipients))

	for _, recipient := range req.Recipients {
		now := time.Now()
		n := &domain.Notification{
			ID:        uuid.NewString(),
			BatchID:   batchID,
			Channel:   req.Channel,
			Recipient: recipient,
			Message:   req.Message,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := s.pipeline.ValidateRecipient(ctx, req.Channel, recipient); err != nil {
			n.Status = domain.StatusFailed
			n.LastError = err.Error()
		} else {
			n.Status = domain.StatusPending
		}

		if err := s.repo.Create(ctx, n); err != nil {
			return nil, fmt.Errorf("service: persist notification: %w", err)
		}

		if n.Status == domain.StatusPending {
			if err := s.queue.Enqueue(n.ID); err != nil {
				n.Status = domain.StatusFailed
				n.LastError = err.Error()
				if updErr := s.repo.Update(ctx, n); updErr != nil {
					s.logger.Error("failed to record queue-full status", "notification_id", n.ID, "error", updErr)
				}
			}
		}

		notifications = append(notifications, n)
	}

	return &BatchResult{BatchID: batchID, Notifications: notifications}, nil
}

// Get returns a single notification by ID.
func (s *NotificationService) Get(ctx context.Context, id string) (*domain.Notification, error) {
	return s.repo.Get(ctx, id)
}

// List returns notifications matching filter along with the total match count.
func (s *NotificationService) List(ctx context.Context, filter store.ListFilter) ([]*domain.Notification, int, error) {
	return s.repo.List(ctx, filter)
}
