package validation_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/validation"
)

// blockingValidator records whether it observed cancellation. It is how the
// tests below prove that a failed validation actually stops the others rather
// than merely ignoring their result.
type blockingValidator struct {
	name     string
	started  chan struct{}
	release  chan struct{}
	err      error
	ran      atomic.Int32
	finished atomic.Bool
	observed atomic.Bool // saw its context cancelled
}

func newBlocking(name string, err error) *blockingValidator {
	return &blockingValidator{
		name:    name,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		err:     err,
	}
}

func (v *blockingValidator) Name() string { return v.name }

func (v *blockingValidator) Validate(ctx context.Context, _ validation.Subject) error {
	v.ran.Add(1)
	select {
	case v.started <- struct{}{}:
	default:
	}

	select {
	case <-v.release:
		v.finished.Store(true)
		return v.err
	case <-ctx.Done():
		v.observed.Store(true)
		return ctx.Err()
	}
}

func subject() validation.Subject {
	return validation.Subject{NotificationID: "ntf_1", DeliveryID: "dlv_1"}
}

func TestPipelineRunsValidatorsConcurrently(t *testing.T) {
	t.Parallel()

	// Each validator waits for all three to have started. If the pipeline ran
	// them sequentially this would deadlock and the test would time out.
	var wg sync.WaitGroup
	wg.Add(3)

	makeValidator := func(name string) validation.Validator {
		return validation.ValidatorFunc{
			ValidatorName: name,
			Fn: func(ctx context.Context, _ validation.Subject) error {
				wg.Done()
				wg.Wait()
				return nil
			},
		}
	}

	pipeline := validation.New(makeValidator("a"), makeValidator("b"), makeValidator("c"))

	done := make(chan error, 1)
	go func() { done <- pipeline.Run(context.Background(), subject()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not finish: validators do not appear to run concurrently")
	}
}

// This is the core requirement: once one validation fails, the others must
// stop consuming resources instead of running to completion.
func TestPipelineCancelsRemainingValidatorsOnFirstFailure(t *testing.T) {
	t.Parallel()

	failing := newBlocking("failing", validation.Reject("bad", "recipient is not acceptable"))
	slowA := newBlocking("slow-a", nil)
	slowB := newBlocking("slow-b", nil)

	pipeline := validation.New(failing, slowA, slowB)

	done := make(chan error, 1)
	go func() { done <- pipeline.Run(context.Background(), subject()) }()

	// Wait until every validator is actually running, then let only the
	// failing one return.
	for _, v := range []*blockingValidator{failing, slowA, slowB} {
		select {
		case <-v.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("validator %q never started", v.Name())
		}
	}
	close(failing.release)

	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after the first failure")
	}

	rejection, ok := validation.AsRejection(err)
	if !ok {
		t.Fatalf("Run() = %v, want a *validation.Rejection", err)
	}
	if rejection.Validator != "failing" {
		t.Errorf("rejection attributed to %q, want %q", rejection.Validator, "failing")
	}
	if !slowA.observed.Load() || !slowB.observed.Load() {
		t.Error("the remaining validators were not cancelled after the first failure")
	}
	if slowA.finished.Load() || slowB.finished.Load() {
		t.Error("the remaining validators ran to completion despite the early failure")
	}
}

func TestPipelineReturnsNilWhenEveryValidatorPasses(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	pass := func(name string) validation.Validator {
		return validation.ValidatorFunc{
			ValidatorName: name,
			Fn: func(context.Context, validation.Subject) error {
				calls.Add(1)
				return nil
			},
		}
	}

	if err := validation.New(pass("a"), pass("b"), pass("c")).Run(context.Background(), subject()); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("ran %d validators, want 3", got)
	}
}

func TestPipelineWithSingleValidatorSkipsTheGoroutine(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	err := validation.New(validation.ValidatorFunc{
		ValidatorName: "only",
		Fn:            func(context.Context, validation.Subject) error { return want },
	}).Run(context.Background(), subject())

	if !errors.Is(err, want) {
		t.Fatalf("Run() = %v, want %v", err, want)
	}
}

func TestPipelineRespectsAnAlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	var ran atomic.Int32
	pipeline := validation.New(
		validation.ValidatorFunc{ValidatorName: "a", Fn: func(context.Context, validation.Subject) error {
			ran.Add(1)
			return nil
		}},
		validation.ValidatorFunc{ValidatorName: "b", Fn: func(context.Context, validation.Subject) error {
			ran.Add(1)
			return nil
		}},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := pipeline.Run(ctx, subject()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
	if got := ran.Load(); got != 0 {
		t.Errorf("ran %d validators on a cancelled context, want 0", got)
	}
}

func TestPipelineWithNoValidatorsPasses(t *testing.T) {
	t.Parallel()

	if err := validation.New().Run(context.Background(), subject()); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}

// A validator that fails because its own dependency is broken must not be
// reported as a rejection: the notification is fine, the check is not.
func TestNonRejectionErrorsAreNotRejections(t *testing.T) {
	t.Parallel()

	want := errors.New("preference service unreachable")
	err := validation.New(validation.ValidatorFunc{
		ValidatorName: "preferences",
		Fn:            func(context.Context, validation.Subject) error { return want },
	}).Run(context.Background(), subject())

	if _, ok := validation.AsRejection(err); ok {
		t.Fatal("an infrastructure error was misclassified as a rejection")
	}
	if !errors.Is(err, want) {
		t.Fatalf("Run() = %v, want it to wrap %v", err, want)
	}
}

func TestRejectionIsNeverRetryable(t *testing.T) {
	t.Parallel()

	r, ok := validation.AsRejection(validation.Reject("code", "reason"))
	if !ok {
		t.Fatal("Reject did not produce a *Rejection")
	}
	if r.Retryable() {
		t.Error("a rejection reported itself as retryable")
	}
}
