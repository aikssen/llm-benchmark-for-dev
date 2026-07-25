package provider

import (
	"errors"
	"fmt"
)

// ErrNoProvider is returned by the registry for an unsupported channel.
var ErrNoProvider = errors.New("no provider registered for channel")

// Failure codes returned by the providers shipped with the service.
// Integrations are free to add their own; the retry machinery only looks at
// the Retryable flag.
const (
	CodeUnavailable  = "provider_unavailable"
	CodeTimeout      = "provider_timeout"
	CodeRateLimited  = "provider_rate_limited"
	CodeRejected     = "provider_rejected"
	CodeInvalidInput = "provider_invalid_input"
)

// Error is a provider failure carrying the one piece of information the
// caller must act on: whether trying again could plausibly succeed.
type Error struct {
	// Provider is the integration that failed.
	Provider string
	// Code is a stable, machine-readable classification.
	Code string
	// Message is the human-readable detail.
	Message string
	// Retry reports whether the failure is transient.
	Retry bool
	// Err is the underlying cause, if any.
	Err error
}

func (e *Error) Error() string {
	if e.Provider == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Provider, e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Retryable implements the interface consulted by internal/resilience.
func (e *Error) Retryable() bool { return e.Retry }

// Transient builds a retryable provider error.
func Transient(name, code, format string, args ...any) *Error {
	return &Error{Provider: name, Code: code, Message: fmt.Sprintf(format, args...), Retry: true}
}

// Permanent builds a non-retryable provider error.
func Permanent(name, code, format string, args ...any) *Error {
	return &Error{Provider: name, Code: code, Message: fmt.Sprintf(format, args...), Retry: false}
}
