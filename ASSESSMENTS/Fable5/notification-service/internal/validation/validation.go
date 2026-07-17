// Package validation runs the independent pre-delivery checks for a single
// delivery (recipient format, user preferences, rate limits, content rules).
//
// Validators run concurrently because they are independent of each other.
// The pipeline is fail-fast: the first failure cancels the context passed to
// the remaining validators so they stop consuming resources.
package validation

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"notificationservice/internal/domain"
)

// Input is everything a validator may need to validate one delivery.
type Input struct {
	Channel   domain.Channel
	Message   domain.Message
	Recipient string
}

// Validator performs one independent validation. Implementations must honor
// ctx cancellation: when another validator fails, ctx is cancelled and any
// in-flight work should be abandoned promptly.
type Validator interface {
	// Name identifies the validator in errors and logs.
	Name() string
	Validate(ctx context.Context, in Input) error
}

// Error wraps a validator failure with the name of the validator that
// produced it.
type Error struct {
	Validator string
	Err       error
}

func (e *Error) Error() string { return fmt.Sprintf("validation %q failed: %v", e.Validator, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// Pipeline runs a fixed set of validators against an input.
type Pipeline struct {
	validators []Validator
}

// NewPipeline builds a pipeline from the given validators.
func NewPipeline(validators ...Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

// Run executes all validators concurrently and returns the first failure as
// a *Error (or nil if all pass). On the first failure the shared context is
// cancelled, so still-running validators can abort early instead of wasting
// resources.
func (p *Pipeline) Run(ctx context.Context, in Input) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, v := range p.validators {
		g.Go(func() error {
			if err := v.Validate(ctx, in); err != nil {
				return &Error{Validator: v.Name(), Err: err}
			}
			return nil
		})
	}
	return g.Wait()
}
