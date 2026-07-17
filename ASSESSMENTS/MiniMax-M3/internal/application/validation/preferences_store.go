package validation

import (
	"context"
	"sync"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
)

type memoryPreferenceStore struct {
	mu   sync.RWMutex
	opts map[string]map[domain.Channel]bool
	now  func() time.Time
}

func NewMemoryPreferenceStore() *memoryPreferenceStore {
	return &memoryPreferenceStore{
		opts: make(map[string]map[domain.Channel]bool),
		now:  time.Now,
	}
}

func (s *memoryPreferenceStore) OptOut(userID string, channel domain.Channel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opts[userID] == nil {
		s.opts[userID] = make(map[domain.Channel]bool)
	}
	s.opts[userID][channel] = true
}

func (s *memoryPreferenceStore) OptIn(userID string, channel domain.Channel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opts[userID] == nil {
		s.opts[userID] = make(map[domain.Channel]bool)
		return
	}
	delete(s.opts[userID], channel)
}

func (s *memoryPreferenceStore) IsOptedOut(_ context.Context, userID string, channel domain.Channel) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	channels, ok := s.opts[userID]
	if !ok {
		return false, nil
	}
	return channels[channel], nil
}
