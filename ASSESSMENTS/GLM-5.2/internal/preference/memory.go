// Package preference provides an in-memory implementation of a user
// preference store. Real implementations would back this with a database.
package preference

import (
	"context"
	"sync"

	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/validation"
)

// MemoryStore is a thread-safe in-memory preference store. Absence of an
// explicit entry means the recipient allows all channels (opt-in by default),
// which is a documented assumption.
type MemoryStore struct {
	mu       sync.RWMutex
	rules    map[string]map[domain.Channel]bool // recipientID -> channel -> allowed
	allowAll bool
}

// NewMemoryStore creates a store. If allowAll is true (default in production
// wiring), recipients without explicit rules allow every channel.
func NewMemoryStore(allowAll bool) *MemoryStore {
	return &MemoryStore{rules: make(map[string]map[domain.Channel]bool), allowAll: allowAll}
}

// Set allows callers (or tests) to override preferences per recipient/channel.
func (s *MemoryStore) Set(recipientID string, channel domain.Channel, allowed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rules[recipientID] == nil {
		s.rules[recipientID] = make(map[domain.Channel]bool)
	}
	s.rules[recipientID][channel] = allowed
}

func (s *MemoryStore) Allows(_ context.Context, recipientID string, channel domain.Channel) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	channels, ok := s.rules[recipientID]
	if !ok {
		return s.allowAll, nil
	}
	allowed, ok := channels[channel]
	if !ok {
		return s.allowAll, nil
	}
	return allowed, nil
}

// Compile-time interface check.
var _ validation.PreferenceStore = (*MemoryStore)(nil)
