package domain

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned when a notification does not exist.
var ErrNotFound = errors.New("notification not found")

// InvalidInputError is a request-level validation failure attributable to a
// specific field. Transports map it to a 4xx response.
//
// Only the first offending field is reported: request validation stops at the
// first failure so that no further work is done on a request that is already
// known to be unacceptable.
type InvalidInputError struct {
	Field   string
	Message string
}

func (e *InvalidInputError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Invalid builds an InvalidInputError.
func Invalid(field, format string, args ...any) error {
	return &InvalidInputError{Field: field, Message: fmt.Sprintf(format, args...)}
}
