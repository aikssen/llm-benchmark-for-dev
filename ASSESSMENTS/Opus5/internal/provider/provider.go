// Package provider defines the boundary between the service and the external
// systems that actually deliver notifications.
//
// Adding a new channel means writing one type that satisfies Provider and
// registering it at start-up. Nothing else in the codebase needs to change:
// the set of accepted channels, the channel-specific address rules and the
// routing of a notification to its provider are all driven by the registry.
package provider

import (
	"context"

	"github.com/evercifuentes/notifier/internal/domain"
)

// Provider delivers messages for exactly one channel.
//
// Implementations must be safe for concurrent use: the worker pool calls Send
// from many goroutines at once.
type Provider interface {
	// Name identifies the concrete integration ("sendgrid", "twilio", ...).
	// It is recorded on the delivery so an operator can trace a message.
	Name() string

	// Channel is the channel this provider serves.
	Channel() domain.Channel

	// ValidateAddress reports whether an address is well formed for this
	// channel. It must be a cheap, purely syntactic check — no I/O — because
	// it runs on the request path before a notification is accepted.
	ValidateAddress(address string) error

	// Send attempts a single delivery. It must honour ctx cancellation and
	// should return an *Error so the caller can tell retryable failures from
	// permanent ones.
	Send(ctx context.Context, req SendRequest) (SendResult, error)
}

// SendRequest is a self-contained instruction to deliver one message to one
// recipient. It carries everything the provider needs so that providers never
// reach back into the service's storage.
type SendRequest struct {
	NotificationID string
	DeliveryID     string
	Recipient      domain.Recipient
	Message        domain.Message
	Metadata       map[string]string
	// Attempt is 1 for the first try and increases with each retry. Providers
	// may use it for their own idempotency keys.
	Attempt int
}

// SendResult is what a provider returns when it accepts a message.
type SendResult struct {
	// MessageID is the provider's own identifier for the accepted message.
	MessageID string
}
