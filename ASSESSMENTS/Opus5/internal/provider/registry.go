package provider

import (
	"fmt"
	"slices"
	"sync"

	"github.com/evercifuentes/notifier/internal/domain"
)

// Registry resolves a channel to the provider that serves it.
//
// It is the single source of truth for "which channels does this deployment
// support?", which is why both request validation and dispatch consult it.
type Registry struct {
	mu        sync.RWMutex
	byChannel map[domain.Channel]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byChannel: make(map[domain.Channel]Provider)}
}

// Register adds a provider. Registering two providers for the same channel is
// a programming error and is reported rather than silently overwritten.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("provider must not be nil")
	}
	ch := p.Channel()
	if ch == "" {
		return fmt.Errorf("provider %q has an empty channel", p.Name())
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byChannel[ch]; ok {
		return fmt.Errorf("channel %q is already served by provider %q", ch, existing.Name())
	}
	r.byChannel[ch] = p
	return nil
}

// MustRegister panics on failure. Intended for start-up wiring only.
func (r *Registry) MustRegister(providers ...Provider) {
	for _, p := range providers {
		if err := r.Register(p); err != nil {
			panic(err)
		}
	}
}

// Get returns the provider for a channel.
func (r *Registry) Get(ch domain.Channel) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byChannel[ch]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoProvider, ch)
	}
	return p, nil
}

// Supports reports whether a channel can be delivered by this deployment.
func (r *Registry) Supports(ch domain.Channel) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byChannel[ch]
	return ok
}

// Channels returns the supported channels, sorted for stable output.
func (r *Registry) Channels() []domain.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Channel, 0, len(r.byChannel))
	for ch := range r.byChannel {
		out = append(out, ch)
	}
	slices.Sort(out)
	return out
}

// Providers returns every registered provider, ordered by channel.
func (r *Registry) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	channels := make([]domain.Channel, 0, len(r.byChannel))
	for ch := range r.byChannel {
		channels = append(channels, ch)
	}
	slices.Sort(channels)
	out := make([]Provider, 0, len(channels))
	for _, ch := range channels {
		out = append(out, r.byChannel[ch])
	}
	return out
}
