// Package provider abstracts the external systems that actually deliver
// notifications (email gateways, SMS carriers, push services).
//
// Everything the rest of the service knows about a provider is captured by the
// Provider interface, so adding a new channel is a matter of implementing that
// interface and registering it — no existing code needs to change.
package provider

import (
	"context"
	"errors"

	"github.com/aikssen/notification-service/internal/domain"
)

// Provider delivers a single notification to an external system. Implementations
// should treat delivery as a single attempt: retry, backoff and circuit
// breaking are handled by the delivery layer, keeping providers thin.
type Provider interface {
	// Channel returns the channel this provider serves.
	Channel() domain.Channel

	// Send attempts one delivery. It must honour ctx cancellation and deadlines
	// (used to bound slow providers) and return an error describing the failure.
	Send(ctx context.Context, n *domain.Notification) error
}

// RetryableError lets a provider signal whether a failure is worth retrying.
// Transient problems (timeouts, 5xx, throttling) are retryable; permanent ones
// (malformed request, hard rejection) are not. The delivery layer inspects this
// to avoid wasting attempts on unrecoverable failures.
type RetryableError struct {
	Err       error
	Retryable bool
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// Retryable wraps err as a retryable failure.
func Retryable(err error) error {
	return &RetryableError{Err: err, Retryable: true}
}

// Permanent wraps err as a non-retryable failure.
func Permanent(err error) error {
	return &RetryableError{Err: err, Retryable: false}
}

// IsRetryable reports whether err should be retried. Errors that do not carry an
// explicit signal default to retryable, since transient failures are the common
// case for network-bound providers and a spurious retry is cheaper than dropping
// a deliverable notification.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if re, ok := errors.AsType[*RetryableError](err); ok {
		return re.Retryable
	}
	return true
}
