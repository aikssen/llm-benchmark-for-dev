package provider

import (
	"fmt"
	"sync"

	"github.com/assessment/notify-service/internal/domain"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel]domain.Provider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[domain.Channel]domain.Provider),
	}
}

func (r *Registry) Register(p domain.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name domain.Channel) (domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("no provider registered for channel %s", name)
	}
	return p, nil
}
