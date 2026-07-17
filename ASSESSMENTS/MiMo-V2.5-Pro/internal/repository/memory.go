package repository

import (
	"context"
	"sort"
	"sync"

	"github.com/notification-service/internal/domain"
	apperrors "github.com/notification-service/pkg/errors"
)

// MemoryRepository is an in-memory implementation of domain.Repository.
// Thread-safe for concurrent access. Suitable for development and testing.
type MemoryRepository struct {
	mu             sync.RWMutex
	notifications  map[string]*domain.Notification
}

// NewMemoryRepository creates a new in-memory repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		notifications: make(map[string]*domain.Notification),
	}
}

// Save persists a notification to memory.
func (r *MemoryRepository) Save(ctx context.Context, notification *domain.Notification) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.notifications[notification.ID]; exists {
		return &apperrors.NotFoundError{Resource: "notification", ID: notification.ID}
	}

	r.notifications[notification.ID] = notification
	return nil
}

// GetByID retrieves a notification by its ID.
func (r *MemoryRepository) GetByID(ctx context.Context, id string) (*domain.Notification, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	notification, exists := r.notifications[id]
	if !exists {
		return nil, &apperrors.NotFoundError{Resource: "notification", ID: id}
	}

	return notification, nil
}

// Update updates an existing notification.
func (r *MemoryRepository) Update(ctx context.Context, notification *domain.Notification) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.notifications[notification.ID]; !exists {
		return &apperrors.NotFoundError{Resource: "notification", ID: notification.ID}
	}

	r.notifications[notification.ID] = notification
	return nil
}

// List retrieves notifications based on the given parameters.
func (r *MemoryRepository) List(ctx context.Context, params domain.ListNotificationsParams) ([]*domain.Notification, int, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*domain.Notification

	for _, n := range r.notifications {
		if params.Channel != "" && n.Channel != params.Channel {
			continue
		}
		if params.Status != "" && n.Status != params.Status {
			continue
		}
		filtered = append(filtered, n)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := len(filtered)

	if params.Offset >= total {
		return []*domain.Notification{}, total, nil
	}

	end := params.Offset + params.Limit
	if end > total || params.Limit == 0 {
		end = total
	}

	return filtered[params.Offset:end], total, nil
}
