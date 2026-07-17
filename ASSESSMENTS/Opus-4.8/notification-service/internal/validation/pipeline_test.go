package validation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

// slowValidator blocks until the context is cancelled or a max duration passes,
// recording whether it ran to completion. It lets us prove the pipeline cancels
// siblings on the first failure.
type slowValidator struct {
	name      string
	completed *atomic.Bool
	dur       time.Duration
}

func (v slowValidator) Name() string { return v.name }

func (v slowValidator) Validate(ctx context.Context, _ *domain.Notification) error {
	select {
	case <-time.After(v.dur):
		v.completed.Store(true)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// failFast fails immediately with a fixed error.
type failFast struct{ name string }

func (v failFast) Name() string { return v.name }
func (v failFast) Validate(context.Context, *domain.Notification) error {
	return errors.New("boom")
}

func TestPipeline_AllPass(t *testing.T) {
	p := NewPipeline(
		ValidatorFunc{ValidatorName: "a", Fn: func(context.Context, *domain.Notification) error { return nil }},
		ValidatorFunc{ValidatorName: "b", Fn: func(context.Context, *domain.Notification) error { return nil }},
	)
	if err := p.Validate(context.Background(), &domain.Notification{}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestPipeline_EmptyPasses(t *testing.T) {
	if err := NewPipeline().Validate(context.Background(), &domain.Notification{}); err != nil {
		t.Fatalf("empty pipeline should pass, got %v", err)
	}
}

func TestPipeline_WrapsValidationSentinel(t *testing.T) {
	p := NewPipeline(failFast{name: "checker"})
	err := p.Validate(context.Background(), &domain.Notification{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("error should wrap domain.ErrValidation, got %v", err)
	}
	if !contains(err.Error(), "checker") {
		t.Errorf("error should name the failing validator, got %v", err)
	}
}

// TestPipeline_FailFastCancelsSiblings is the core guarantee: when one validator
// fails, the others must be cancelled rather than run to completion.
func TestPipeline_FailFastCancelsSiblings(t *testing.T) {
	var completed atomic.Bool
	slow := slowValidator{name: "slow", completed: &completed, dur: 2 * time.Second}
	p := NewPipeline(failFast{name: "fast"}, slow)

	start := time.Now()
	err := p.Validate(context.Background(), &domain.Notification{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected validation error")
	}
	if completed.Load() {
		t.Error("slow validator ran to completion; it should have been cancelled")
	}
	if elapsed > time.Second {
		t.Errorf("pipeline did not fail fast; took %v", elapsed)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
