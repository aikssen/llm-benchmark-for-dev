package domain

import "errors"

// Sentinel errors shared across layers. Handlers map these to HTTP status
// codes; keeping them here avoids the API layer importing store/service
// internals just to do error-type switches.
var (
	// ErrNotFound is returned when a notification lookup misses.
	ErrNotFound = errors.New("notification not found")
	// ErrValidation is returned when request-level validation fails
	// (malformed input, unsupported channel, empty message, etc).
	ErrValidation = errors.New("validation failed")
	// ErrQueueFull is returned when the delivery queue has no spare
	// capacity and cannot accept more work without unbounded buffering.
	ErrQueueFull = errors.New("delivery queue is full")
)
