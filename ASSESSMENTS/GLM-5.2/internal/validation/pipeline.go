// Package validation implements the independent validation steps that must
// pass before a notification is delivered.
//
// Validators are independent of each other (they do not depend on one
// another's results). The Pipeline runs them concurrently and, as soon as one
// fails, cancels the remaining ones so they do not keep consuming resources
// (fail-fast).
package validation

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"

	"github.com/example/notification-service/internal/domain"
)

// Validator is a single, independent validation step.
type Validator interface {
	// Name returns a short, stable identifier for the validator, useful for
	// logging and metrics.
	Name() string
	// Validate returns a *domain.ValidationErr when the input is invalid. It
	// must respect ctx so the Pipeline can cancel in-flight work on fail-fast.
	Validate(ctx context.Context, in Input) error
}

// Input is the data every validator operates on.
type Input struct {
	Notification *domain.Notification
	Recipient    domain.Recipient
}

// Pipeline runs a set of independent validators concurrently with fail-fast
// semantics: the first failure cancels the remaining in-flight validators.
type Pipeline struct {
	validators []Validator
}

// NewPipeline builds a Pipeline from the given validators. Order does not
// affect correctness because they run concurrently; it only affects which
// failure is reported first on races.
func NewPipeline(validators ...Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

// Run executes all validators concurrently. It returns the validation errors
// observed. On fail-fast, typically one error is returned, but because
// validators run concurrently a second one may race in just before
// cancellation propagates; all observed errors are returned.
func (p *Pipeline) Run(ctx context.Context, in Input) []domain.ValidationError {
	if len(p.validators) == 0 {
		return nil
	}
	g, gctx := errgroup.WithContext(ctx)
	errs := make(chan domain.ValidationError, len(p.validators))

	for _, v := range p.validators {
		v := v
		g.Go(func() error {
			err := v.Validate(gctx, in)
			if err == nil {
				return nil
			}
			// A sibling that failed fast cancels the shared context; validators
			// that observe the cancellation return context.Canceled. That is an
			// artifact of fail-fast, not a genuine validation error, so we do
			// not surface it to clients.
			if errors.Is(err, context.Canceled) {
				return nil
			}
			errs <- domain.AsValidationError(err)
			return err // cancels gctx for the remaining goroutines
		})
	}

	_ = g.Wait()
	close(errs)

	out := make([]domain.ValidationError, 0, len(p.validators))
	for e := range errs {
		out = append(out, e)
	}
	return out
}

// IsValidation reports whether err is a domain validation error.
func IsValidation(err error) bool {
	var ve *domain.ValidationErr
	return errors.As(err, &ve)
}
