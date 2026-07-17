package store

import (
	"context"
	"sync"

	"github.com/aikssen/notification-service/internal/domain"
)

// MemoryRepository is a thread-safe, in-memory Repository implementation.
// Notifications are kept in insertion order in a slice (for cheap
// chronological listing) alongside a map for O(1) point lookups. Every
// method returns copies so callers can never mutate stored state through a
// pointer obtained from Get/List.
type MemoryRepository struct {
	mu      sync.RWMutex
	byID    map[string]*domain.Notification
	ordered []string // notification IDs in insertion order
}

// NewMemoryRepository builds an empty repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		byID: make(map[string]*domain.Notification),
	}
}

func (r *MemoryRepository) Create(_ context.Context, n *domain.Notification) error {
	cp := *n
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[cp.ID] = &cp
	r.ordered = append(r.ordered, cp.ID)
	return nil
}

func (r *MemoryRepository) Update(_ context.Context, n *domain.Notification) error {
	cp := *n
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[cp.ID]; !ok {
		return domain.ErrNotFound
	}
	r.byID[cp.ID] = &cp
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, id string) (*domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *n
	return &cp, nil
}

func (r *MemoryRepository) List(_ context.Context, filter ListFilter) ([]*domain.Notification, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matched := make([]*domain.Notification, 0, len(r.ordered))
	for i := len(r.ordered) - 1; i >= 0; i-- { // newest first
		n := r.byID[r.ordered[i]]
		if !matches(n, filter) {
			continue
		}
		cp := *n
		matched = append(matched, &cp)
	}

	total := len(matched)

	offset := max(filter.Offset, 0)
	if offset >= total {
		return []*domain.Notification{}, total, nil
	}

	end := total
	if filter.Limit > 0 && offset+filter.Limit < end {
		end = offset + filter.Limit
	}

	return matched[offset:end], total, nil
}

func matches(n *domain.Notification, f ListFilter) bool {
	if f.BatchID != "" && n.BatchID != f.BatchID {
		return false
	}
	if f.Status != "" && n.Status != f.Status {
		return false
	}
	if f.Channel != "" && n.Channel != f.Channel {
		return false
	}
	if f.Recipient != "" && n.Recipient != f.Recipient {
		return false
	}
	return true
}
