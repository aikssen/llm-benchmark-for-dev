package store

import (
	"context"
	"sort"
	"sync"

	"github.com/aikssen/notification-service/internal/domain"
)

// defaultListLimit caps the number of items returned when a caller does not
// specify a limit, protecting the API from unbounded response sizes.
const defaultListLimit = 50

// Memory is an in-memory, concurrency-safe Store. It is intended for local
// development, tests and demos. State is lost on restart; a production
// deployment would back the same Store interface with a durable database.
type Memory struct {
	mu    sync.RWMutex
	items map[string]domain.Notification
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{items: make(map[string]domain.Notification)}
}

// Create implements Store. Notifications are stored by value so that callers
// cannot mutate stored state through a retained pointer.
func (m *Memory) Create(_ context.Context, n *domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[n.ID]; exists {
		return errAlreadyExists
	}
	m.items[n.ID] = *n
	return nil
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, id string) (*domain.Notification, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &n, nil
}

// Update implements Store.
func (m *Memory) Update(_ context.Context, n *domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[n.ID]; !ok {
		return domain.ErrNotFound
	}
	m.items[n.ID] = *n
	return nil
}

// List implements Store. Results are sorted newest-first by creation time, with
// the ID as a tie-breaker to keep ordering deterministic under equal timestamps.
func (m *Memory) List(_ context.Context, f ListFilter) ([]*domain.Notification, int, error) {
	m.mu.RLock()
	matches := make([]domain.Notification, 0, len(m.items))
	for _, n := range m.items {
		if matchesFilter(n, f) {
			matches = append(matches, n)
		}
	}
	m.mu.RUnlock()

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].CreatedAt.Equal(matches[j].CreatedAt) {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})

	total := len(matches)
	page := paginate(matches, f.Offset, f.Limit)

	out := make([]*domain.Notification, len(page))
	for i := range page {
		n := page[i]
		out[i] = &n
	}
	return out, total, nil
}

func matchesFilter(n domain.Notification, f ListFilter) bool {
	if f.Channel != "" && n.Channel != f.Channel {
		return false
	}
	if f.Status != "" && n.Status != f.Status {
		return false
	}
	if f.Recipient != "" && n.Recipient != f.Recipient {
		return false
	}
	return true
}

// paginate applies offset/limit to an already-sorted slice, clamping bounds so
// out-of-range values yield an empty page rather than a panic.
func paginate(items []domain.Notification, offset, limit int) []domain.Notification {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return nil
	}
	end := min(offset+limit, len(items))
	return items[offset:end]
}
