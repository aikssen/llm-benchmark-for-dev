package provider

import (
	"fmt"
	"sync"

	"github.com/notification-service/internal/domain"
)

// Registry manages notification providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel]domain.Provider
}

// NewRegistry creates a new provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[domain.Channel]domain.Provider),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(provider domain.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.Channel()] = provider
}

// GetProvider returns the provider for the given channel.
func (r *Registry) GetProvider(channel domain.Channel) (domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[channel]
	if !exists {
		return nil, fmt.Errorf("no provider registered for channel: %s", channel)
	}

	return provider, nil
}
