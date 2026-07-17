package validation

import (
	"context"
	"errors"
	"testing"
	"time"

	"notificationservice/internal/domain"
)

type fakeValidator struct {
	name string
	fn   func(ctx context.Context, in Input) error
}

func (f fakeValidator) Name() string                                 { return f.name }
func (f fakeValidator) Validate(ctx context.Context, in Input) error { return f.fn(ctx, in) }

func TestPipelineAllPass(t *testing.T) {
	ok := fakeValidator{name: "ok", fn: func(context.Context, Input) error { return nil }}
	p := NewPipeline(ok, ok, ok)
	if err := p.Run(context.Background(), Input{}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestPipelineReturnsWrappedFailure(t *testing.T) {
	boom := errors.New("boom")
	p := NewPipeline(
		fakeValidator{name: "ok", fn: func(context.Context, Input) error { return nil }},
		fakeValidator{name: "bad", fn: func(context.Context, Input) error { return boom }},
	)
	err := p.Run(context.Background(), Input{})
	if err == nil {
		t.Fatal("expected failure")
	}
	var verr *Error
	if !errors.As(err, &verr) {
		t.Fatalf("expected *validation.Error, got %T", err)
	}
	if verr.Validator != "bad" || !errors.Is(err, boom) {
		t.Errorf("unexpected error: %+v", verr)
	}
}

// TestPipelineFailFast asserts the core requirement: when one validation
// fails, the others are cancelled instead of running to completion.
func TestPipelineFailFast(t *testing.T) {
	slowCancelled := make(chan bool, 1)
	slow := fakeValidator{name: "slow", fn: func(ctx context.Context, _ Input) error {
		select {
		case <-ctx.Done():
			slowCancelled <- true
			return ctx.Err()
		case <-time.After(5 * time.Second):
			slowCancelled <- false
			return nil
		}
	}}
	failing := fakeValidator{name: "failing", fn: func(context.Context, Input) error {
		return errors.New("rejected")
	}}

	start := time.Now()
	err := NewPipeline(slow, failing).Run(context.Background(), Input{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected failure")
	}
	if elapsed > time.Second {
		t.Errorf("pipeline took %v; the failure should have cancelled the slow validator", elapsed)
	}
	if cancelled := <-slowCancelled; !cancelled {
		t.Error("slow validator ran to completion instead of being cancelled")
	}
	// The reported error must be the real failure, not the cancellation.
	var verr *Error
	if !errors.As(err, &verr) || verr.Validator != "failing" {
		t.Errorf("expected the failing validator's error, got %v", err)
	}
}

func TestPipelineHonorsParentContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := fakeValidator{name: "blocked", fn: func(ctx context.Context, _ Input) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	if err := NewPipeline(blocked).Run(ctx, Input{Channel: domain.ChannelEmail}); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
