package store

import (
	"context"
	"sync"

	"notifications/internal/domain"
)

// DefaultListLimit is used when a filter does not specify a limit.
const DefaultListLimit = 50

// Memory is a goroutine-safe in-memory Store. It is adequate for local
// development and tests; a production deployment would swap in a durable
// implementation of the same interface.
type Memory struct {
	mu    sync.RWMutex
	byID  map[string]*domain.Notification
	order []string // insertion order for List
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]*domain.Notification)}
}

func (m *Memory) Upsert(_ context.Context, n *domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byID[n.ID]; !exists {
		m.order = append(m.order, n.ID)
	}
	m.byID[n.ID] = n.Clone()
	return nil
}

func (m *Memory) Get(_ context.Context, id string) (*domain.Notification, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n.Clone(), nil
}

func (m *Memory) List(_ context.Context, f ListFilter) ([]*domain.Notification, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}

	out := make([]*domain.Notification, 0, limit)
	skipped := 0
	for _, id := range m.order {
		n := m.byID[id]
		if !matches(n, f) {
			continue
		}
		if skipped < f.Offset {
			skipped++
			continue
		}
		out = append(out, n.Clone())
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func matches(n *domain.Notification, f ListFilter) bool {
	if f.Status != "" && n.Status != f.Status {
		return false
	}
	if f.Channel != "" && n.Channel != f.Channel {
		return false
	}
	if f.RequestID != "" && n.RequestID != f.RequestID {
		return false
	}
	return true
}
