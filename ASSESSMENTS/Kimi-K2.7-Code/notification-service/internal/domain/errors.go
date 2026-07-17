package domain

import "errors"

// Domain-level errors returned by the notification service.
var (
	ErrNotFound           = errors.New("notification not found")
	ErrInvalidRecipient   = errors.New("invalid recipient")
	ErrInvalidMessage     = errors.New("invalid message content")
	ErrInvalidRequest     = errors.New("invalid request")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrPreferenceBlocked  = errors.New("notification blocked by user preference")
	ErrProviderFailed     = errors.New("provider delivery failed")
	ErrNoProvider         = errors.New("no provider available for channel")
)
