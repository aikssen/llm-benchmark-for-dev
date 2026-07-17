package validation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"notifications/internal/domain"
)

// stubValidator is a configurable test validator.
type stubValidator struct {
	name string
	err  error
	// block signals on start and waits until released or ctx cancelled.
	block  chan struct{}
	called *atomic.Int32
}

func (s stubValidator) Name() string { return s.name }

func (s stubValidator) Validate(ctx context.Context, _ *domain.Notification) error {
	if s.called != nil {
		s.called.Add(1)
	}
	if s.block != nil {
		close(s.block)
		select {
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
}

func TestPipelineAllPass(t *testing.T) {
	p := NewPipeline(
		stubValidator{name: "a"},
		stubValidator{name: "b"},
		stubValidator{name: "c"},
	)
	n := domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Body: "x"}, time.Now())
	if err := p.Validate(context.Background(), n); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestPipelineReportsFailure(t *testing.T) {
	p := NewPipeline(
		stubValidator{name: "a"},
		stubValidator{name: "b", err: errors.New("boom")},
	)
	n := domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Body: "x"}, time.Now())
	err := p.Validate(context.Background(), n)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRejection(err) {
		t.Fatalf("expected a Rejection, got %T: %v", err, err)
	}
	if got := RejectionSummary(err); got != "b: boom" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

// TestPipelineFailFastCancelsSlowValidators is the key behavioural test:
// when one validator fails quickly, a slow sibling must be cancelled via
// context instead of running to completion.
func TestPipelineFailFastCancelsSlowValidators(t *testing.T) {
	slowStarted := make(chan struct{})
	p := NewPipeline(
		stubValidator{name: "fast-failing", err: errors.New("bad recipient")},
		stubValidator{name: "slow", block: slowStarted}, // would block forever without cancellation
	)
	n := domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Body: "x"}, time.Now())

	done := make(chan error, 1)
	go func() { done <- p.Validate(context.Background(), n) }()

	// Wait until the slow validator is actually running, so the test
	// proves it was interrupted mid-flight.
	<-slowStarted

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected pipeline to fail")
		}
		if !IsRejection(err) {
			t.Fatalf("expected rejection, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not return promptly after a validator failed; fail-fast is broken")
	}
}

func TestPipelineContextErrorFromCancellationIsNotReported(t *testing.T) {
	// The slow validator returns ctx.Err() when cancelled; the pipeline
	// must surface only the real rejection.
	slowStarted := make(chan struct{})
	p := NewPipeline(
		stubValidator{name: "failing", err: errors.New("real reason")},
		stubValidator{name: "slow", block: slowStarted},
	)
	n := domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Body: "x"}, time.Now())
	err := p.Validate(context.Background(), n)
	if got := RejectionSummary(err); got != "failing: real reason" {
		t.Fatalf("expected only the real rejection, got %q", got)
	}
}
