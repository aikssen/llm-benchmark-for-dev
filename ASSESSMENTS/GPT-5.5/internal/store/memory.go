package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"notification-service/internal/domain"
)

var ErrNotFound = errors.New("notification not found")

type ListFilter struct {
	Status  domain.Status
	Channel domain.Channel
	Limit   int
}

type MemoryStore struct {
	mu            sync.RWMutex
	notifications map[string]domain.Notification
	order         []string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		notifications: make(map[string]domain.Notification),
		order:         make([]string, 0),
	}
}

func (s *MemoryStore) Save(_ context.Context, notification domain.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	notification = clone(notification)
	if _, exists := s.notifications[notification.ID]; !exists {
		s.order = append(s.order, notification.ID)
	}
	s.notifications[notification.ID] = notification
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	notification, ok := s.notifications[id]
	if !ok {
		return domain.Notification{}, ErrNotFound
	}
	return clone(notification), nil
}

func (s *MemoryStore) List(_ context.Context, filter ListFilter) ([]domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	results := make([]domain.Notification, 0, min(limit, len(s.notifications)))
	for i := len(s.order) - 1; i >= 0 && len(results) < limit; i-- {
		notification := s.notifications[s.order[i]]
		if filter.Status != "" && notification.Status != filter.Status {
			continue
		}
		if filter.Channel != "" && notification.Channel != filter.Channel {
			continue
		}
		results = append(results, clone(notification))
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	return results, nil
}

func (s *MemoryStore) Update(ctx context.Context, id string, update func(*domain.Notification)) (domain.Notification, error) {
	select {
	case <-ctx.Done():
		return domain.Notification{}, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	notification, ok := s.notifications[id]
	if !ok {
		return domain.Notification{}, ErrNotFound
	}

	update(&notification)
	notification.UpdatedAt = time.Now().UTC()
	s.notifications[id] = notification
	return clone(notification), nil
}

func clone(notification domain.Notification) domain.Notification {
	copy := notification
	if notification.Message.Data != nil {
		copy.Message.Data = make(map[string]string, len(notification.Message.Data))
		for key, value := range notification.Message.Data {
			copy.Message.Data[key] = value
		}
	}
	return copy
}
