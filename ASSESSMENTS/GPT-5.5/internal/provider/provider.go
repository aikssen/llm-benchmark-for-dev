package provider

import (
	"context"
	"errors"
	"sync"

	"notification-service/internal/domain"
)

var ErrProviderNotFound = errors.New("provider not found for channel")

type Sender interface {
	Name() string
	Send(ctx context.Context, notification domain.Notification) error
}

type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel]Sender
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.Channel]Sender)}
}

func (r *Registry) Register(channel domain.Channel, sender Sender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[channel] = sender
}

func (r *Registry) Get(channel domain.Channel) (Sender, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sender, ok := r.providers[channel]
	return sender, ok
}

func (r *Registry) Has(channel domain.Channel) bool {
	_, ok := r.Get(channel)
	return ok
}
