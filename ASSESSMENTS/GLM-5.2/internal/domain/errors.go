package domain

import "errors"

// Sentinel errors used across layer boundaries. Infrastructure code should
// wrap these with context but keep them identifiable via errors.Is.
var (
	ErrNotFound             = errors.New("notification not found")
	ErrQueueFull            = errors.New("processing queue full")
	ErrChannelUnsupported   = errors.New("channel not supported")
	ErrInvalidRequest       = errors.New("invalid request")
	ErrRecipientUnreachable = errors.New("recipient unreachable")
)

// ValidationErr is returned by validators. It carries a stable machine-readable
// code so callers do not need to parse free-text messages.
type ValidationErr struct {
	Code    string
	Field   string
	Message string
}

func (e *ValidationErr) Error() string {
	if e.Field != "" {
		return e.Code + " [" + e.Field + "]: " + e.Message
	}
	return e.Code + ": " + e.Message
}

func (e *ValidationErr) ToValidationError() ValidationError {
	return ValidationError{Code: e.Code, Field: e.Field, Message: e.Message}
}

// NewValidationError is a convenience constructor.
func NewValidationError(code, field, message string) *ValidationErr {
	return &ValidationErr{Code: code, Field: field, Message: message}
}

// AsValidationError unwraps an error into a ValidationError. If the error is
// not a *ValidationErr, it is reported with a generic code.
func AsValidationError(err error) ValidationError {
	var ve *ValidationErr
	if ok := errors.As(err, &ve); ok {
		return ve.ToValidationError()
	}
	return ValidationError{Code: "validation_failed", Message: err.Error()}
}
