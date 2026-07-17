package validation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-orchestrator/internal/domain"
	"github.com/example/notification-orchestrator/internal/provider"
	"github.com/example/notification-orchestrator/internal/validation"
)

func TestPipelineCancelsOtherValidatorsAfterFailure(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	pipeline := validation.NewPipeline(2,
		validator{name: "blocking", validate: func(ctx context.Context, _ provider.Payload) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return ctx.Err()
		}},
		validator{name: "failing", validate: func(context.Context, provider.Payload) error {
			<-started
			return errors.New("not allowed")
		}},
	)
	err := pipeline.Validate(context.Background(), provider.Payload{Channel: domain.ChannelEmail})
	if err == nil || err.Error() != "failing: not allowed" {
		t.Fatalf("Validate() error = %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("blocking validator did not receive cancellation")
	}
}

func TestRecipientFormat(t *testing.T) {
	validator := validation.RecipientFormat{}
	if err := validator.Validate(context.Background(), provider.Payload{Channel: domain.ChannelSMS, Recipient: "+573001234567"}); err != nil {
		t.Fatalf("valid phone rejected: %v", err)
	}
	if err := validator.Validate(context.Background(), provider.Payload{Channel: domain.ChannelSMS, Recipient: "3001234567"}); err == nil {
		t.Fatal("invalid phone accepted")
	}
}

type validator struct {
	name     string
	validate func(context.Context, provider.Payload) error
}

func (v validator) Name() string { return v.name }
func (v validator) Validate(ctx context.Context, payload provider.Payload) error {
	return v.validate(ctx, payload)
}
