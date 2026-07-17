// Package channel maps domain notifications to provider Outbound messages and
// delegates the actual send to a provider.Provider.
//
// A Channel is concerned with: selecting the right recipient address for the
// channel, shaping the message, and calling the provider. The unreliable
// delivery concerns (retry, circuit breaker, timeout) are handled by provider
// middleware wrapping the provider, so channels stay thin.
//
// New channels are added by implementing Channel and registering it in the
// Registry at the composition root (see cmd/server). Existing channels require
// no modification.
package channel

import (
	"context"
	"fmt"

	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/provider"
)

// Channel delivers a notification to a single recipient.
type Channel interface {
	Name() domain.Channel
	Send(ctx context.Context, n *domain.Notification, r domain.Recipient) error
}

// Registry maps a domain.Channel to its Channel implementation.
type Registry struct {
	channels map[domain.Channel]Channel
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{channels: make(map[domain.Channel]Channel)}
}

// Register adds (or replaces) a channel. Returning the registry enables a
// fluent composition at startup.
func (r *Registry) Register(c Channel) *Registry {
	r.channels[c.Name()] = c
	return r
}

// Get returns the channel for the given domain channel.
func (r *Registry) Get(c domain.Channel) (Channel, error) {
	ch, ok := r.channels[c]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrChannelUnsupported, c)
	}
	return ch, nil
}

// Supported returns the list of registered channel names.
func (r *Registry) Supported() []domain.Channel {
	out := make([]domain.Channel, 0, len(r.channels))
	for c := range r.channels {
		out = append(out, c)
	}
	return out
}

// Email delivers notifications via an email provider.
type Email struct{ provider provider.Provider }

// NewEmail builds an email channel backed by the given provider.
func NewEmail(p provider.Provider) *Email { return &Email{provider: p} }

func (e *Email) Name() domain.Channel { return domain.ChannelEmail }

func (e *Email) Send(ctx context.Context, n *domain.Notification, r domain.Recipient) error {
	return e.provider.Send(ctx, provider.Outbound{
		To:      r.Email,
		Subject: n.Message.Subject,
		Body:    n.Message.Body,
	})
}

// SMS delivers notifications via an SMS provider.
type SMS struct{ provider provider.Provider }

// NewSMS builds an SMS channel backed by the given provider.
func NewSMS(p provider.Provider) *SMS { return &SMS{provider: p} }

func (s *SMS) Name() domain.Channel { return domain.ChannelSMS }

func (s *SMS) Send(ctx context.Context, n *domain.Notification, r domain.Recipient) error {
	return s.provider.Send(ctx, provider.Outbound{
		To:   r.Phone,
		Body: n.Message.Body,
	})
}

// Push delivers notifications via a push provider.
type Push struct{ provider provider.Provider }

// NewPush builds a push channel backed by the given provider.
func NewPush(p provider.Provider) *Push { return &Push{provider: p} }

func (p *Push) Name() domain.Channel { return domain.ChannelPush }

func (p *Push) Send(ctx context.Context, n *domain.Notification, r domain.Recipient) error {
	return p.provider.Send(ctx, provider.Outbound{
		To:   r.PushToken,
		Body: n.Message.Body,
		Data: n.Message.Data,
	})
}

// Compile-time interface checks.
var (
	_ Channel = (*Email)(nil)
	_ Channel = (*SMS)(nil)
	_ Channel = (*Push)(nil)
)
