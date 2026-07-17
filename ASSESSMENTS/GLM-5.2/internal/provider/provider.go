// Package provider abstracts external notification delivery systems.
//
// A Provider is the low-level sender for a given channel (e.g. an SMTP server,
// an SMS gateway). Providers are wrapped with middleware (timeout, retry,
// circuit breaker) to absorb the unreliability described in the requirements.
//
// Channels (see package channel) translate domain notifications into Outbound
// messages and delegate to a Provider, keeping channel logic decoupled from the
// particular vendor implementation. New vendors providers can be added by
// implementing Provider and wiring it with the desired middleware.
package provider

import (
	"context"
)

// Outbound is the channel-agnostic payload handed to a provider.
type Outbound struct {
	To      string
	Subject string
	Body    string
	Data    map[string]string
}

// Provider sends an outbound message. Implementations must be safe for
// concurrent use.
type Provider interface {
	Name() string
	Send(ctx context.Context, out Outbound) error
}

// Middleware decorates a Provider. Middlewares compose, e.g.:
//
//	cb(retry(timeout(base)))
type Middleware func(Provider) Provider
