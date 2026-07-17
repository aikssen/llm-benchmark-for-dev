// Package provider abstracts the external systems that actually deliver
// notifications (email gateways, SMS aggregators, push services, ...).
//
// Providers are unreliable: they can be slow, fail intermittently or be
// unavailable. ResilientProvider wraps any Provider with a per-attempt
// timeout, retry with exponential backoff and jitter, and a circuit
// breaker, so the rest of the system only ever deals with a reliable
// Send contract. Adding support for a new channel means implementing
// Provider and registering it in the Registry — nothing else changes.
package provider

import (
	"context"
	"errors"
	"fmt"

	"notifications/internal/domain"
)

// Error classifies a provider failure as temporary (worth retrying) or
// permanent (retrying would be pointless, e.g. unknown recipient).
type Error struct {
	Temporary bool
	Reason    string
}

func (e *Error) Error() string { return e.Reason }

// Temporaryf builds a retryable provider error.
func Temporaryf(format string, args ...any) *Error {
	return &Error{Temporary: true, Reason: fmt.Sprintf(format, args...)}
}

// Permanentf builds a non-retryable provider error.
func Permanentf(format string, args ...any) *Error {
	return &Error{Temporary: false, Reason: fmt.Sprintf(format, args...)}
}

// IsTemporary reports whether err is a retryable provider error. Unknown
// error types are treated as temporary (fail-safe: prefer retry over
// silently dropping a notification).
func IsTemporary(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Temporary
	}
	return true
}

// Provider delivers a single notification through one channel.
type Provider interface {
	// Channel returns the channel this provider serves.
	Channel() domain.Channel
	// Send attempts one delivery. It must respect ctx cancellation and
	// return an *Error to classify failures.
	Send(ctx context.Context, n *domain.Notification) error
}

// Registry maps channels to providers.
type Registry struct {
	providers map[domain.Channel]Provider
}

// NewRegistry builds a registry from the given providers.
func NewRegistry(providers ...Provider) (*Registry, error) {
	r := &Registry{providers: make(map[domain.Channel]Provider, len(providers))}
	for _, p := range providers {
		ch := p.Channel()
		if _, dup := r.providers[ch]; dup {
			return nil, fmt.Errorf("provider: duplicate provider for channel %q", ch)
		}
		r.providers[ch] = p
	}
	return r, nil
}

// Get returns the provider registered for ch.
func (r *Registry) Get(ch domain.Channel) (Provider, bool) {
	p, ok := r.providers[ch]
	return p, ok
}

// Supports reports whether ch has a registered provider. The HTTP layer
// uses it to validate the requested channel, so new channels become
// valid API inputs automatically once registered.
func (r *Registry) Supports(ch domain.Channel) bool {
	_, ok := r.providers[ch]
	return ok
}

// Channels lists the registered channels.
func (r *Registry) Channels() []domain.Channel {
	out := make([]domain.Channel, 0, len(r.providers))
	for ch := range r.providers {
		out = append(out, ch)
	}
	return out
}
