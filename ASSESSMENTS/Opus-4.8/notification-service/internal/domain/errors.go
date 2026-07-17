package domain

import "errors"

// Sentinel errors shared across layers. Callers use errors.Is to branch on them
// (e.g. the API maps ErrNotFound to 404 and ErrValidation to 400) without
// depending on concrete error types.
var (
	// ErrNotFound is returned by the store when a notification does not exist.
	ErrNotFound = errors.New("notification not found")

	// ErrValidation indicates a request was rejected before processing. It is
	// typically wrapped with a human-readable reason.
	ErrValidation = errors.New("validation failed")

	// ErrNoProvider indicates no provider is registered for a channel.
	ErrNoProvider = errors.New("no provider registered for channel")
)
