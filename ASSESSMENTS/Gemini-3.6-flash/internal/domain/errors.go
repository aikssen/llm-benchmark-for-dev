package domain

import "errors"

var (
	ErrNotificationNotFound  = errors.New("notification request not found")
	ErrInvalidChannel        = errors.New("unsupported or invalid notification channel")
	ErrEmptyRecipients       = errors.New("recipients list cannot be empty")
	ErrEmptyContent          = errors.New("message content body cannot be empty")
	ErrInvalidRecipient      = errors.New("invalid recipient format")
	ErrRateLimitExceeded     = errors.New("rate limit exceeded for recipient")
	ErrUserOptedOut          = errors.New("user has opted out of notifications")
	ErrNoProviderAvailable   = errors.New("no available provider for channel")
	ErrMaxRetriesExceeded    = errors.New("max delivery retries exceeded")
	ErrCircuitBreakerOpen    = errors.New("circuit breaker is open for provider")
	ErrValidationFailed      = errors.New("notification validation failed")
	ErrInvalidPaginationArgs = errors.New("invalid pagination arguments")
)
