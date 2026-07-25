// Package preferences stores per-recipient notification settings.
//
// In a real deployment this would be a client of the user-profile service. It
// is modelled here as a small port with an in-memory adapter so the rest of
// the system can be built, run and tested against it today.
package preferences

import (
	"context"
	"maps"
	"sync"

	"github.com/evercifuentes/notifier/internal/domain"
)

// Preferences is what a recipient has chosen.
type Preferences struct {
	// OptedOut lists channels the recipient refuses. A channel absent from
	// the map is allowed: the default is opt-in, matching transactional
	// notification semantics.
	OptedOut map[domain.Channel]bool
	// Blocked suppresses every channel at once (account closed, hard bounce,
	// abuse report).
	Blocked bool
}

// Allows reports whether the recipient accepts notifications on a channel.
func (p Preferences) Allows(ch domain.Channel) bool {
	return !p.Blocked && !p.OptedOut[ch]
}

// Store resolves a recipient's preferences.
//
// It returns the zero value (everything allowed) for unknown recipients
// rather than an error, because the service must not refuse a transactional
// notification just because the recipient has never set a preference.
type Store interface {
	Get(ctx context.Context, recipientKey string) (Preferences, error)
}

// MemoryStore is a concurrency-safe in-memory Store.
type MemoryStore struct {
	mu    sync.RWMutex
	prefs map[string]Preferences
}

// Compile-time proof that the adapter satisfies the port.
var _ Store = (*MemoryStore)(nil)

// NewMemoryStore builds a store seeded with the given preferences.
func NewMemoryStore(seed map[string]Preferences) *MemoryStore {
	prefs := make(map[string]Preferences, len(seed))
	maps.Copy(prefs, seed)
	return &MemoryStore{prefs: prefs}
}

// Get returns the recipient's preferences, or the permissive default.
func (s *MemoryStore) Get(_ context.Context, recipientKey string) (Preferences, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefs[recipientKey], nil
}

// Set records preferences for a recipient.
func (s *MemoryStore) Set(recipientKey string, p Preferences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefs[recipientKey] = p
}
