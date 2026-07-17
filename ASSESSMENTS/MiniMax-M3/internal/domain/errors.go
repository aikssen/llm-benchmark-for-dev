package domain

import "errors"

var (
	ErrUnknownChannel   = errors.New("unknown channel")
	ErrNotFound         = errors.New("notification not found")
	ErrInvalidRequest   = errors.New("invalid request")
	ErrRateLimited      = errors.New("rate limited")
	ErrUserOptedOut     = errors.New("recipient opted out of channel")
	ErrInvalidRecipient = errors.New("invalid recipient")
	ErrInvalidMessage   = errors.New("invalid message")
	ErrProviderUnavail  = errors.New("provider unavailable")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func NewValidationError(field, msg string) *ValidationError {
	return &ValidationError{Field: field, Message: msg}
}
