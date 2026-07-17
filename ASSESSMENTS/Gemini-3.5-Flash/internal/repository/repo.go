package repository

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"notification-service/internal/domain"
)

var (
	ErrNotFound = errors.New("notification not found")
)

type Repository interface {
	Save(ctx context.Context, n *domain.Notification) error
	Get(ctx context.Context, id string) (*domain.Notification, error)
	List(ctx context.Context) ([]*domain.Notification, error)
	Update(ctx context.Context, n *domain.Notification) error
}

type InMemoryRepository struct {
	mu            sync.RWMutex
	notifications map[string]*domain.Notification
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		notifications: make(map[string]*domain.Notification),
	}
}

func (r *InMemoryRepository) Save(ctx context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Deep copy to prevent side effects
	cp := copyNotification(n)
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	r.notifications[n.ID] = cp

	// Update the original object with set timestamps
	n.CreatedAt = cp.CreatedAt
	n.UpdatedAt = cp.UpdatedAt
	return nil
}

func (r *InMemoryRepository) Get(ctx context.Context, id string) (*domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n, exists := r.notifications[id]
	if !exists {
		return nil, ErrNotFound
	}

	return copyNotification(n), nil
}

func (r *InMemoryRepository) List(ctx context.Context) ([]*domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*domain.Notification, 0, len(r.notifications))
	for _, n := range r.notifications {
		list = append(list, copyNotification(n))
	}

	// Sort by CreatedAt descending (newest first)
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	return list, nil
}

func (r *InMemoryRepository) Update(ctx context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.notifications[n.ID]
	if !exists {
		return ErrNotFound
	}

	cp := copyNotification(n)
	cp.UpdatedAt = time.Now()
	r.notifications[n.ID] = cp

	n.UpdatedAt = cp.UpdatedAt
	return nil
}

func copyNotification(n *domain.Notification) *domain.Notification {
	return &domain.Notification{
		ID:          n.ID,
		RequestID:   n.RequestID,
		Recipient:   n.Recipient,
		Channel:     n.Channel,
		Content:     n.Content,
		Status:      n.Status,
		ErrorReason: n.ErrorReason,
		RetryCount:  n.RetryCount,
		CreatedAt:   n.CreatedAt,
		UpdatedAt:   n.UpdatedAt,
	}
}
