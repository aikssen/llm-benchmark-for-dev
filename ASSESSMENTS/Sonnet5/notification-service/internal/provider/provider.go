// Package provider abstracts external notification delivery systems
// (email/SMS/push gateways). Providers are unreliable by nature — slow,
// intermittently failing, sometimes fully unavailable — so callers (see
// internal/delivery) wrap Send with timeouts, retries, and a circuit
// breaker rather than trusting it to behave.
//
// To add a new channel: implement Provider, then register an instance
// against the new domain.Channel in main.go. No existing code changes.
package provider

import (
	"context"

	"github.com/aikssen/notification-service/internal/domain"
)

// Provider delivers a single notification through one channel.
type Provider interface {
	Channel() domain.Channel
	Send(ctx context.Context, n domain.Notification) error
}

// Registry maps a channel to the Provider responsible for it.
type Registry struct {
	providers map[domain.Channel]Provider
}

// NewRegistry builds a Registry from a set of providers, keyed by their
// own Channel(). Registering two providers for the same channel is a
// programming error and panics immediately at startup rather than
// silently shadowing one of them.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: make(map[domain.Channel]Provider, len(providers))}
	for _, p := range providers {
		if _, exists := r.providers[p.Channel()]; exists {
			panic("provider: duplicate registration for channel " + string(p.Channel()))
		}
		r.providers[p.Channel()] = p
	}
	return r
}

// Get returns the provider registered for channel, if any.
func (r *Registry) Get(channel domain.Channel) (Provider, bool) {
	p, ok := r.providers[channel]
	return p, ok
}
