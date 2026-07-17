package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
)

var (
	// ErrNotFound is returned when a notification does not exist.
	ErrNotFound = errors.New("notification not found")
)

// Store persists notifications. Implementations must be safe for concurrent use.
type Store interface {
	Create(ctx context.Context, n *domain.Notification) error
	Get(ctx context.Context, id string) (*domain.Notification, error)
	List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, int, error)
	Update(ctx context.Context, n *domain.Notification) error
}

// MemoryStore is an in-memory Store suitable for local development and tests.
type MemoryStore struct {
	mu    sync.RWMutex
	items map[string]*domain.Notification
	order []string
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		items: make(map[string]*domain.Notification),
		order: make([]string, 0),
	}
}

// Create inserts a new notification.
func (s *MemoryStore) Create(_ context.Context, n *domain.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[n.ID]; exists {
		return errors.New("notification already exists")
	}

	cp := *n
	s.items[n.ID] = &cp
	s.order = append(s.order, n.ID)
	return nil
}

// Get returns a notification by ID.
func (s *MemoryStore) Get(_ context.Context, id string) (*domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n, ok := s.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *n
	return &cp, nil
}

// List returns notifications matching the filter (newest first).
func (s *MemoryStore) List(_ context.Context, filter domain.ListFilter) ([]*domain.Notification, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]*domain.Notification, 0)
	for i := len(s.order) - 1; i >= 0; i-- {
		n := s.items[s.order[i]]
		if filter.Status != "" && n.Status != filter.Status {
			continue
		}
		if filter.Channel != "" && n.Channel != filter.Channel {
			continue
		}
		cp := *n
		matched = append(matched, &cp)
	}

	total := len(matched)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Offset >= total {
		return []*domain.Notification{}, total, nil
	}
	end := filter.Offset + filter.Limit
	if end > total {
		end = total
	}
	return matched[filter.Offset:end], total, nil
}

// Update replaces an existing notification.
func (s *MemoryStore) Update(_ context.Context, n *domain.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[n.ID]; !ok {
		return ErrNotFound
	}
	n.UpdatedAt = time.Now().UTC()
	cp := *n
	s.items[n.ID] = &cp
	return nil
}
