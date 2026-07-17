package validation

import (
	"context"
	"fmt"
	"sync"

	"github.com/aikssen/notification-service/internal/domain"
)

// PreferenceStore answers whether a recipient has opted out of a channel.
// A real implementation would query a user-preferences service/database;
// InMemoryPreferenceStore simulates one so the pipeline can be exercised
// end-to-end without external dependencies.
type PreferenceStore interface {
	IsOptedOut(recipient string, channel domain.Channel) bool
}

// InMemoryPreferenceStore is a thread-safe, in-memory PreferenceStore.
type InMemoryPreferenceStore struct {
	mu      sync.RWMutex
	optOuts map[string]map[domain.Channel]bool
}

// NewInMemoryPreferenceStore builds a store with no opt-outs recorded.
func NewInMemoryPreferenceStore() *InMemoryPreferenceStore {
	return &InMemoryPreferenceStore{optOuts: make(map[string]map[domain.Channel]bool)}
}

// OptOut records that recipient no longer wants notifications on channel.
// Exposed mainly for tests and for demonstrating the validator's behavior.
func (s *InMemoryPreferenceStore) OptOut(recipient string, channel domain.Channel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.optOuts[recipient] == nil {
		s.optOuts[recipient] = make(map[domain.Channel]bool)
	}
	s.optOuts[recipient][channel] = true
}

func (s *InMemoryPreferenceStore) IsOptedOut(recipient string, channel domain.Channel) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.optOuts[recipient][channel]
}

// PreferenceValidator rejects recipients who have opted out of the
// requested channel. It runs after format validation (which is purely
// local) since it simulates a lookup against another system.
type PreferenceValidator struct {
	Store PreferenceStore
}

func (PreferenceValidator) Name() string { return "preference" }

func (v PreferenceValidator) Validate(_ context.Context, channel domain.Channel, recipient string) error {
	if v.Store.IsOptedOut(recipient, channel) {
		return fmt.Errorf("%w: recipient %q has opted out of %s notifications", domain.ErrValidation, recipient, channel)
	}
	return nil
}
