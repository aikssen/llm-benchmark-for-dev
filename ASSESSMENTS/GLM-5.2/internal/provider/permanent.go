package provider

import "errors"

// permanentError wraps an error that must not be retried (e.g. invalid
// recipient rejected synchronously by the vendor API).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so retry middleware skips it.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err was marked permanent.
func IsPermanent(err error) bool {
	var p *permanentError
	return errors.As(err, &p)
}
