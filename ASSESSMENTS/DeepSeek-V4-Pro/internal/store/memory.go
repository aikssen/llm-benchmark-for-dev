package store

import (
	"context"
	"sync"
	"sort"

	"github.com/assessment/notify-service/internal/domain"
)

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]*domain.Notification
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]*domain.Notification),
	}
}

func (s *MemoryStore) Save(_ context.Context, n *domain.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[n.ID] = n
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.data[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return n, nil
}

func (s *MemoryStore) List(_ context.Context, offset, limit int) ([]*domain.Notification, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]*domain.Notification, 0, len(s.data))
	for _, n := range s.data {
		items = append(items, n)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	total := len(items)

	if offset >= total {
		return nil, total, nil
	}
	if offset+limit > total {
		limit = total - offset
	}

	return items[offset : offset+limit], total, nil
}
