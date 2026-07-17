// Package provider defines the boundary to external notification providers
// (email, SMS, push, ...) and a simulated implementation for local runs.
//
// Adding a new channel/provider only requires implementing Provider and
// registering it in the Registry at startup; no other code changes.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"notificationservice/internal/domain"
)

// Provider sends a notification to a single recipient over one channel.
//
// Send must honor ctx (timeouts/cancellation). Errors are considered
// transient (retryable) unless wrapped with Permanent.
type Provider interface {
	// Name identifies the concrete provider in logs (e.g. "sim-email").
	Name() string
	// Channel is the channel this provider serves.
	Channel() domain.Channel
	Send(ctx context.Context, n domain.Notification, recipient string) error
}

// ErrNoProvider is returned by Registry.Get for channels without a
// registered provider.
var ErrNoProvider = errors.New("no provider registered for channel")

// Registry maps channels to providers. One provider per channel; routing or
// failover across multiple providers per channel is a documented future
// extension.
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.Channel]Provider)}
}

// Register adds a provider for its channel. Registering a second provider
// for the same channel is a wiring bug and returns an error.
func (r *Registry) Register(p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.providers[p.Channel()]; ok {
		return fmt.Errorf("channel %q already has provider %q", p.Channel(), existing.Name())
	}
	r.providers[p.Channel()] = p
	return nil
}

// Get returns the provider for a channel or ErrNoProvider.
func (r *Registry) Get(ch domain.Channel) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[ch]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoProvider, ch)
	}
	return p, nil
}

// permanentError marks an error as not retryable (e.g. invalid recipient
// rejected by the provider, hard bounce).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so IsPermanent reports true. A nil err returns nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err (or any error it wraps) was marked
// permanent. Permanent errors must not be retried.
func IsPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}
