package repository

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
)

type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
	Get(ctx context.Context, id string) (*domain.Notification, error)
	Update(ctx context.Context, n *domain.Notification) error
	List(ctx context.Context, filter ListFilter) ([]*domain.Notification, error)
}

type ListFilter struct {
	Channel domain.Channel
	Status  domain.Status
	Limit   int
	Offset  int
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]*domain.Notification
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]*domain.Notification)}
}

func (r *MemoryRepository) Create(_ context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *n
	if _, exists := r.items[n.ID]; exists {
		return domain.NewValidationError("id", "duplicate id")
	}
	r.items[n.ID] = &clone
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, id string) (*domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	clone := *n
	return &clone, nil
}

func (r *MemoryRepository) Update(_ context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[n.ID]; !ok {
		return domain.ErrNotFound
	}
	clone := *n
	r.items[n.ID] = &clone
	return nil
}

func (r *MemoryRepository) List(_ context.Context, f ListFilter) ([]*domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.Notification, 0, len(r.items))
	for _, n := range r.items {
		if f.Channel != "" && n.Channel != f.Channel {
			continue
		}
		if f.Status != "" && n.Status != f.Status {
			continue
		}
		c := *n
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			return []*domain.Notification{}, nil
		}
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (r *MemoryRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

func (r *MemoryRepository) SweepOlderThan(_ context.Context, ttl time.Duration, now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for id, n := range r.items {
		if !n.Status.Terminal() {
			continue
		}
		if now.Sub(n.UpdatedAt) > ttl {
			delete(r.items, id)
			removed++
		}
	}
	return removed
}
