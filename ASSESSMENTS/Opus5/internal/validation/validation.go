// Package validation runs the checks that decide whether a delivery is
// allowed to reach a provider.
//
// The checks are independent of each other, so the pipeline runs them
// concurrently. As soon as one of them fails the shared context is cancelled,
// which stops the others: a delivery that is already known to be
// unacceptable must not keep burning CPU, database round-trips or rate-limit
// budget on the remaining checks.
package validation

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/evercifuentes/notifier/internal/domain"
)

// Subject is the immutable input to every validator. It is a flat snapshot
// rather than the Notification aggregate so that validators cannot mutate
// persisted state and do not need storage access.
type Subject struct {
	NotificationID string
	DeliveryID     string
	Channel        domain.Channel
	Recipient      domain.Recipient
	Message        domain.Message
	Metadata       map[string]string
}

// Validator performs one independent check.
//
// Implementations must be safe for concurrent use and must abandon their work
// when ctx is cancelled — that is what makes the fail-fast behaviour actually
// save resources.
type Validator interface {
	// Name identifies the check in logs and in the rejection reason.
	Name() string
	// Validate returns nil when the delivery is acceptable, a *Rejection when
	// it must be refused permanently, and any other error when the check
	// itself could not be completed.
	Validate(ctx context.Context, s Subject) error
}

// ValidatorFunc adapts a function to the Validator interface.
type ValidatorFunc struct {
	ValidatorName string
	Fn            func(ctx context.Context, s Subject) error
}

func (v ValidatorFunc) Name() string { return v.ValidatorName }

func (v ValidatorFunc) Validate(ctx context.Context, s Subject) error { return v.Fn(ctx, s) }

// Rejection is a permanent refusal: the delivery violates a rule and retrying
// it would produce the same answer. It is distinct from an infrastructure
// error inside a validator, which is transient and must not be recorded as a
// rejection.
type Rejection struct {
	// Validator is filled in by the pipeline.
	Validator string
	// Code is a stable, machine-readable reason.
	Code string
	// Reason is the human-readable detail.
	Reason string
}

func (r *Rejection) Error() string {
	if r.Validator == "" {
		return fmt.Sprintf("%s: %s", r.Code, r.Reason)
	}
	return fmt.Sprintf("%s rejected the delivery: %s: %s", r.Validator, r.Code, r.Reason)
}

// Retryable implements the interface consulted by internal/resilience: a
// rejection is never worth retrying.
func (r *Rejection) Retryable() bool { return false }

// Reject builds a Rejection. The pipeline attributes it to the validator that
// returned it, so callers do not set the Validator field themselves.
func Reject(code, format string, args ...any) error {
	return &Rejection{Code: code, Reason: fmt.Sprintf(format, args...)}
}

// AsRejection reports whether err is a permanent rejection.
func AsRejection(err error) (*Rejection, bool) {
	var r *Rejection
	ok := errors.As(err, &r)
	return r, ok
}

// Pipeline runs a fixed set of validators concurrently, failing fast.
//
// The zero value is not usable; build one with New.
type Pipeline struct {
	validators []Validator
}

// New returns a pipeline over the given validators. Order is irrelevant: they
// all start at the same time.
func New(validators ...Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

// Run executes every validator concurrently and returns the first failure.
//
// On failure the derived context is cancelled immediately, so the validators
// that are still running observe cancellation and return without finishing
// their work. Run still waits for them: letting goroutines outlive the call
// would make resource usage unpredictable, which is the opposite of the goal.
func (p *Pipeline) Run(ctx context.Context, s Subject) error {
	if len(p.validators) == 0 {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// A single validator does not need a goroutine or a cancellation scope.
	if len(p.validators) == 1 {
		return attribute(p.validators[0], p.validators[0].Validate(ctx, s))
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for _, v := range p.validators {
		wg.Add(1)
		go func(v Validator) {
			defer wg.Done()
			if err := v.Validate(runCtx, s); err != nil {
				once.Do(func() {
					firstErr = attribute(v, err)
					// Stop the remaining validators: their answer can no
					// longer change the outcome.
					cancel()
				})
			}
		}(v)
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	// Every validator passed; surface a cancellation of the *parent* context
	// if one happened, so a caller that gave up is not told "all good".
	return ctx.Err()
}

// attribute records which validator produced a rejection.
func attribute(v Validator, err error) error {
	if err == nil {
		return nil
	}
	if r, ok := AsRejection(err); ok && r.Validator == "" {
		r.Validator = v.Name()
	}
	return err
}
