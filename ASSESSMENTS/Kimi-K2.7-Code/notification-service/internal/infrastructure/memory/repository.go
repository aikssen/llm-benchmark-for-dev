package memory

import (
	"context"
	"sort"
	"sync"

	"notification-service/internal/domain"
)

// NotificationRepository is a thread-safe in-memory implementation of domain.NotificationRepository.
// It is suitable for local development and tests; production deployments should swap it for a persistent store.
type NotificationRepository struct {
	mu            sync.RWMutex
	notifications map[string]domain.Notification
	order         []string
}

// NewNotificationRepository creates a new in-memory repository.
func NewNotificationRepository() *NotificationRepository {
	return &NotificationRepository{
		notifications: make(map[string]domain.Notification),
		order:         make([]string, 0),
	}
}

// Save persists the notification.
func (r *NotificationRepository) Save(_ context.Context, n domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.notifications[n.ID]; !exists {
		r.order = append(r.order, n.ID)
	}
	r.notifications[n.ID] = n
	return nil
}

// GetByID retrieves a notification by ID.
func (r *NotificationRepository) GetByID(_ context.Context, id string) (domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n, ok := r.notifications[id]
	if !ok {
		return domain.Notification{}, domain.ErrNotFound
	}
	return n, nil
}

// List returns notifications sorted by creation time descending.
func (r *NotificationRepository) List(_ context.Context, limit, offset int) ([]domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if offset >= len(r.order) {
		return []domain.Notification{}, nil
	}

	// order is insertion order (chronological). Reverse for descending.
	end := offset + limit
	if end > len(r.order) {
		end = len(r.order)
	}

	ids := r.order[offset:end]
	out := make([]domain.Notification, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		out = append(out, r.notifications[ids[i]])
	}

	// Fallback sort in case timestamps drift.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	return out, nil
}
