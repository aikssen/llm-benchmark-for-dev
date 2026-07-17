package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
	"github.com/evercifuentes/notifications/internal/store"
	"github.com/evercifuentes/notifications/internal/worker"
	"github.com/google/uuid"
)

// ErrInvalidRequest is returned for client input errors.
var ErrInvalidRequest = errors.New("invalid request")

// ErrQueueFull is returned when the system is under backpressure.
var ErrQueueFull = errors.New("notification queue is full")

// Service orchestrates notification creation and querying.
type Service struct {
	store store.Store
	pool  *worker.Pool
}

// New creates a notification service.
func New(st store.Store, pool *worker.Pool) *Service {
	return &Service{store: st, pool: pool}
}

// Submit accepts a multi-recipient request, fans out into per-recipient
// notifications, and enqueues them for async processing.
func (s *Service) Submit(ctx context.Context, req domain.CreateRequest) (*domain.CreateResponse, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	requestID := uuid.NewString()
	now := time.Now().UTC()
	ids := make([]string, 0, len(req.Recipients))

	// De-duplicate recipients while preserving order.
	seen := make(map[string]struct{}, len(req.Recipients))
	unique := make([]string, 0, len(req.Recipients))
	for _, r := range req.Recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		unique = append(unique, r)
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("%w: at least one recipient is required", ErrInvalidRequest)
	}

	notifications := make([]*domain.Notification, 0, len(unique))
	for _, recipient := range unique {
		n := &domain.Notification{
			ID:        uuid.NewString(),
			RequestID: requestID,
			Recipient: recipient,
			Channel:   req.Channel,
			Subject:   strings.TrimSpace(req.Subject),
			Body:      strings.TrimSpace(req.Body),
			Status:    domain.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.store.Create(ctx, n); err != nil {
			return nil, fmt.Errorf("persist notification: %w", err)
		}
		notifications = append(notifications, n)
		ids = append(ids, n.ID)
	}

	// Enqueue after all are persisted so clients can always query by ID.
	for _, n := range notifications {
		if ok := s.pool.Enqueue(ctx, worker.Job{NotificationID: n.ID}); !ok {
			n.Status = domain.StatusFailed
			n.LastError = ErrQueueFull.Error()
			_ = s.store.Update(ctx, n)
			return nil, ErrQueueFull
		}
	}

	return &domain.CreateResponse{
		RequestID:       requestID,
		NotificationIDs: ids,
		Status:          domain.StatusPending,
	}, nil
}

// Get returns a notification by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.Notification, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidRequest)
	}
	return s.store.Get(ctx, id)
}

// List returns notifications matching the filter.
func (s *Service) List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return s.store.List(ctx, filter)
}

func validateCreateRequest(req domain.CreateRequest) error {
	if len(req.Recipients) == 0 {
		return fmt.Errorf("%w: recipients is required", ErrInvalidRequest)
	}
	if !domain.IsValidChannel(req.Channel) {
		return fmt.Errorf("%w: channel must be one of email, sms, push", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Body) == "" {
		return fmt.Errorf("%w: body is required", ErrInvalidRequest)
	}
	return nil
}
