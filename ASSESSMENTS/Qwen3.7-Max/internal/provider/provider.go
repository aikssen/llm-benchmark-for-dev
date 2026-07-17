package provider

import (
	"context"

	"github.com/notifications-service/internal/domain"
)

type Provider interface {
	Send(ctx context.Context, recipient string, subject string, message string) error
	Channel() domain.Channel
}

type Registry struct {
	providers map[domain.Channel]Provider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[domain.Channel]Provider),
	}
}

func (r *Registry) Register(p Provider) {
	r.providers[p.Channel()] = p
}

func (r *Registry) Get(channel domain.Channel) (Provider, bool) {
	p, ok := r.providers[channel]
	return p, ok
}
