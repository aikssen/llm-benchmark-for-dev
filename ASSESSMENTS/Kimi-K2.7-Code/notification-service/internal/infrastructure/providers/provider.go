package providers

import (
	"context"

	"notification-service/internal/domain"
)

// Provider abstracts a concrete notification delivery backend.
type Provider interface {
	// Name returns a human-readable identifier for observability.
	Name() string

	// Channel returns the channel this provider handles.
	Channel() domain.Channel

	// Send attempts to deliver the notification content to the recipient.
	Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error
}

// Registry maps channels to their configured providers.
type Registry map[domain.Channel]Provider

// Get returns the provider for the given channel, if any.
func (r Registry) Get(channel domain.Channel) (Provider, bool) {
	p, ok := r[channel]
	return p, ok
}

// Register adds a provider to the registry.
func (r Registry) Register(p Provider) {
	r[p.Channel()] = p
}
