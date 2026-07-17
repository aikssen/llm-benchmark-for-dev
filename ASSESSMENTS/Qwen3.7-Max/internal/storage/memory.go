package storage

import (
	"context"
	"sort"
	"sync"

	"github.com/notifications-service/internal/domain"
)

type MemoryStore struct {
	mu             sync.RWMutex
	notifications  map[string]*domain.Notification
	recipientStats map[string][]*domain.RecipientNotification
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		notifications:  make(map[string]*domain.Notification),
		recipientStats: make(map[string][]*domain.RecipientNotification),
	}
}

func (s *MemoryStore) Save(_ context.Context, notification *domain.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.notifications[notification.ID] = notification
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n, ok := s.notifications[id]
	if !ok {
		return nil, domain.ErrNotificationNotFound
	}
	return n, nil
}

func (s *MemoryStore) List(_ context.Context, filter domain.ListFilter) ([]*domain.Notification, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*domain.Notification
	for _, n := range s.notifications {
		if filter.Channel != nil && n.Channel != *filter.Channel {
			continue
		}
		if filter.Status != nil && n.Status != *filter.Status {
			continue
		}
		result = append(result, n)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	total := len(result)

	if filter.Offset >= len(result) {
		return nil, total, nil
	}
	result = result[filter.Offset:]

	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}

	return result, total, nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id string, status domain.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, ok := s.notifications[id]
	if !ok {
		return domain.ErrNotificationNotFound
	}
	n.Status = status
	return nil
}

func (s *MemoryStore) SaveRecipientStatus(_ context.Context, rs *domain.RecipientNotification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recipientStats[rs.NotificationID] = append(s.recipientStats[rs.NotificationID], rs)
	return nil
}

func (s *MemoryStore) GetRecipientStatuses(_ context.Context, notificationID string) ([]*domain.RecipientNotification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.recipientStats[notificationID], nil
}
