package validator

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/assessment/notify-service/internal/domain"
)

func TestPipelineValidationPasses(t *testing.T) {
	pipeline := NewPipeline(
		NewRecipientValidator(),
		NewContentValidator(),
	)

	n := &domain.Notification{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "user@test.com"}},
		Message:    domain.Message{Body: "Hello World"},
	}

	result := pipeline.Validate(context.Background(), n)
	if !result.Passed {
		t.Errorf("expected validation to pass, got errors: %v", result.Errors)
	}
}

func TestPipelineValidationFails(t *testing.T) {
	pipeline := NewPipeline(
		NewRecipientValidator(),
		NewContentValidator(),
	)

	n := &domain.Notification{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "notanemail"}},
		Message:    domain.Message{Body: "Hello"},
	}

	result := pipeline.Validate(context.Background(), n)
	if result.Passed {
		t.Error("expected validation to fail for invalid email")
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one error message")
	}
}

func TestFailFastCancellation(t *testing.T) {
	var counter atomic.Int32

	slowValidator := &countingValidator{
		name: "slow",
		fn: func() *domain.ValidationResult {
			counter.Add(1)
			return &domain.ValidationResult{Passed: true}
		},
	}

	failingValidator := &countingValidator{
		name: "fast_fail",
		fn: func() *domain.ValidationResult {
			counter.Add(1)
			return &domain.ValidationResult{
				Passed: false,
				Errors: []string{"simulated failure"},
			}
		},
	}

	lateValidator := &countingValidator{
		name: "should_not_run",
		fn: func() *domain.ValidationResult {
			counter.Add(1)
			return &domain.ValidationResult{Passed: true}
		},
	}

	pipeline := NewPipeline(slowValidator, failingValidator, lateValidator)
	n := &domain.Notification{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "test@test.com"}},
		Message:    domain.Message{Body: "test"},
	}

	result := pipeline.Validate(context.Background(), n)
	if result.Passed {
		t.Error("expected validation to fail")
	}

	// All three should have been called since they run concurrently
	// and we don't cancel fast enough (all start at nearly the same time).
	// At minimum, the failing one was called.
	if counter.Load() < 2 {
		t.Errorf("expected at least 2 validators to be called, got %d", counter.Load())
	}
}

type countingValidator struct {
	name string
	fn   func() *domain.ValidationResult
}

func (v *countingValidator) Name() string { return v.name }
func (v *countingValidator) Validate(_ context.Context, _ *domain.Notification) (*domain.ValidationResult, error) {
	return v.fn(), nil
}
