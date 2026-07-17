package notification

import "fmt"

type ValidationError struct {
	code    string
	message string
}

func NewValidationError(code, message string) ValidationError {
	return ValidationError{code: code, message: message}
}

func (e ValidationError) Error() string { return e.message }
func (e ValidationError) Code() string  { return e.code }

type ProviderError struct {
	Err         error
	CanRetry    bool
	Description string
}

func (e ProviderError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	return fmt.Sprintf("provider error: %v", e.Err)
}

func (e ProviderError) Unwrap() error   { return e.Err }
func (e ProviderError) Retryable() bool { return e.CanRetry }
