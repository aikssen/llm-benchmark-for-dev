package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"notificationservice/internal/domain"
)

// Memory is a thread-safe in-memory Store. Notifications are kept in
// insertion order so listings are stable and newest-first.
type Memory struct {
	mu    sync.RWMutex
	byID  map[string]*domain.Notification
	order []string // notification IDs in creation order

	now func() time.Time // injectable for tests
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		byID: make(map[string]*domain.Notification),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (m *Memory) CreateNotification(_ context.Context, n domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byID[n.ID]; exists {
		return fmt.Errorf("notification %q already exists", n.ID)
	}
	stored := cloneNotification(n)
	m.byID[n.ID] = &stored
	m.order = append(m.order, n.ID)
	return nil
}

func (m *Memory) DeleteNotification(_ context.Context, notificationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byID[notificationID]; !exists {
		return ErrNotFound
	}
	delete(m.byID, notificationID)
	if i := slices.Index(m.order, notificationID); i >= 0 {
		m.order = slices.Delete(m.order, i, i+1)
	}
	return nil
}

func (m *Memory) GetNotification(_ context.Context, notificationID string) (domain.Notification, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	n, ok := m.byID[notificationID]
	if !ok {
		return domain.Notification{}, ErrNotFound
	}
	return cloneNotification(*n), nil
}

func (m *Memory) ListNotifications(_ context.Context, f ListFilter) (ListResult, error) {
	if f.Limit <= 0 {
		return ListResult{}, fmt.Errorf("list: limit must be positive, got %d", f.Limit)
	}
	if f.Offset < 0 {
		return ListResult{}, fmt.Errorf("list: offset must not be negative, got %d", f.Offset)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var matches []*domain.Notification
	// Newest first: walk creation order backwards.
	for i := len(m.order) - 1; i >= 0; i-- {
		n := m.byID[m.order[i]]
		if f.Channel != "" && n.Channel != f.Channel {
			continue
		}
		if f.Status != "" && n.Status() != f.Status {
			continue
		}
		matches = append(matches, n)
	}

	result := ListResult{Total: len(matches)}
	if f.Offset >= len(matches) {
		return result, nil
	}
	end := min(f.Offset+f.Limit, len(matches))
	for _, n := range matches[f.Offset:end] {
		result.Notifications = append(result.Notifications, cloneNotification(*n))
	}
	return result, nil
}

func (m *Memory) UpdateDelivery(_ context.Context, notificationID, deliveryID string, fn func(*domain.Delivery)) (domain.Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n, ok := m.byID[notificationID]
	if !ok {
		return domain.Delivery{}, ErrNotFound
	}
	for i := range n.Deliveries {
		if n.Deliveries[i].ID == deliveryID {
			fn(&n.Deliveries[i])
			n.Deliveries[i].UpdatedAt = m.now()
			return n.Deliveries[i], nil
		}
	}
	return domain.Delivery{}, ErrNotFound
}

// cloneNotification deep-copies a notification so no mutable state is shared
// across the store boundary.
func cloneNotification(n domain.Notification) domain.Notification {
	n.Deliveries = slices.Clone(n.Deliveries)
	return n
}
