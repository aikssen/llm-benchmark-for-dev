// Package validation implements the pre-delivery validation pipeline.
//
// Validators are independent of each other, so the pipeline runs them
// concurrently. As soon as one validator rejects the notification the
// shared context is cancelled so the remaining validators stop consuming
// resources (fail-fast). Adding a new validation rule means implementing
// the Validator interface and registering it; no other code changes.
package validation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"notifications/internal/domain"
)

// Validator is a single, independent pre-delivery check.
//
// Implementations MUST honour ctx cancellation and return promptly when
// the context is cancelled; that is what allows the pipeline to free
// resources as soon as any other validator fails.
type Validator interface {
	Name() string
	Validate(ctx context.Context, n *domain.Notification) error
}

// Rejection describes a single failed validation in human-readable form.
type Rejection struct {
	Validator string
	Reason    string
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: %s", e.Validator, e.Reason)
}

// IsRejection reports whether err contains at least one Rejection.
func IsRejection(err error) bool {
	var r *Rejection
	return errors.As(err, &r)
}

// RejectionSummary flattens err into a short human-readable summary.
func RejectionSummary(err error) string {
	var parts []string
	for _, e := range flatten(err) {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

func flatten(err error) []error {
	type unwrapper interface{ Unwrap() []error }
	if u, ok := err.(unwrapper); ok {
		var out []error
		for _, e := range u.Unwrap() {
			out = append(out, flatten(e)...)
		}
		return out
	}
	return []error{err}
}

// Pipeline runs a set of validators concurrently with fail-fast
// semantics: the first failure cancels the rest.
type Pipeline struct {
	validators []Validator
}

// NewPipeline builds a pipeline from the given validators. Order only
// affects error reporting, not execution.
func NewPipeline(validators ...Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

// Validate runs every validator against n. It returns nil when all pass.
// On failure it returns the joined Rejection(s) observed before
// cancellation took effect; usually exactly one, but validators racing
// to fail may produce more.
func (p *Pipeline) Validate(ctx context.Context, n *domain.Notification) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, len(p.validators))

	for i, v := range p.validators {
		wg.Add(1)
		go func(i int, v Validator) {
			defer wg.Done()
			if err := v.Validate(ctx, n); err != nil {
				// Ignore errors caused by our own cancellation: they are
				// not real rejections, just validators stopping early.
				if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
					return
				}
				errs[i] = &Rejection{Validator: v.Name(), Reason: err.Error()}
				cancel() // fail-fast: stop the remaining validators
			}
		}(i, v)
	}
	wg.Wait()

	return errors.Join(errs...)
}
