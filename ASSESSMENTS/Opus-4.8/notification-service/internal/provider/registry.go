package provider

import (
	"fmt"
	"sync"

	"github.com/aikssen/notification-service/internal/domain"
)

// Registry maps channels to their providers. It is the single lookup point the
// delivery layer uses, so wiring a new channel means registering it here at
// startup and nothing else.
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.Channel]Provider)}
}

// Register adds a provider for its channel, replacing any existing one. It
// panics on a nil provider because that is a programming error at wiring time,
// not a runtime condition to be handled.
func (r *Registry) Register(p Provider) {
	if p == nil {
		panic("provider: cannot register nil provider")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Channel()] = p
}

// Get returns the provider for a channel, or domain.ErrNoProvider if none is
// registered.
func (r *Registry) Get(c domain.Channel) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[c]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrNoProvider, c)
	}
	return p, nil
}

// Channels returns the set of channels that have a registered provider. Useful
// for validating requests up front and for health/introspection endpoints.
func (r *Registry) Channels() []domain.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Channel, 0, len(r.providers))
	for c := range r.providers {
		out = append(out, c)
	}
	return out
}
