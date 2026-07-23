package validator

import (
	"context"
	"errors"
	"testing"
	"time"

	"notification-service/internal/domain"
)

func TestValidationPipeline_FastFailing(t *testing.T) {
	fastErr := errors.New("immediate failure")

	failVal := &stubValidator{
		name: "FastFail",
		fn: func(ctx context.Context, item *domain.SingleRecipientItem) error {
			return fastErr
		},
	}

	slowStarted := make(chan struct{})
	slowCancelled := make(chan struct{})

	slowVal := &stubValidator{
		name: "SlowValidation",
		fn: func(ctx context.Context, item *domain.SingleRecipientItem) error {
			close(slowStarted)
			select {
			case <-ctx.Done():
				close(slowCancelled)
				return ctx.Err()
			case <-time.After(2 * time.Second):
				return nil
			}
		},
	}

	pipeline := NewPipeline(failVal, slowVal)

	item := &domain.SingleRecipientItem{
		Recipient: "test@example.com",
		Channel:   domain.ChannelEmail,
		Body:      "Hello world",
	}

	start := time.Now()
	err := pipeline.Validate(context.Background(), item)
	duration := time.Since(start)

	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if duration > 500*time.Millisecond {
		t.Fatalf("pipeline took %v, expected fast fail within < 500ms", duration)
	}

	select {
	case <-slowCancelled:
		// Success! Slow validator was cancelled immediately when fast validator failed
	case <-time.After(1 * time.Second):
		t.Fatalf("slow validator was not cancelled")
	}
}

func TestRecipientValidator(t *testing.T) {
	rv := NewRecipientValidator()

	tests := []struct {
		name      string
		recipient string
		channel   domain.Channel
		wantErr   bool
	}{
		{"Valid Email", "user@example.com", domain.ChannelEmail, false},
		{"Invalid Email", "not-an-email", domain.ChannelEmail, true},
		{"Valid SMS", "+14155552671", domain.ChannelSMS, false},
		{"Invalid SMS", "abc1234", domain.ChannelSMS, true},
		{"Valid Push", "token-abcdef123456", domain.ChannelPush, false},
		{"Invalid Push", "short", domain.ChannelPush, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &domain.SingleRecipientItem{
				Recipient: tt.recipient,
				Channel:   tt.channel,
			}
			err := rv.Validate(context.Background(), item)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContentValidator(t *testing.T) {
	cv := NewContentValidator()

	itemSMS := &domain.SingleRecipientItem{
		Channel: domain.ChannelSMS,
		Body:    string(make([]byte, 170)), // > 160
	}
	if err := cv.Validate(context.Background(), itemSMS); err == nil {
		t.Errorf("expected error for oversized SMS body")
	}

	itemEmail := &domain.SingleRecipientItem{
		Channel: domain.ChannelEmail,
		Body:    "Valid email content",
	}
	if err := cv.Validate(context.Background(), itemEmail); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

type stubValidator struct {
	name string
	fn   func(ctx context.Context, item *domain.SingleRecipientItem) error
}

func (s *stubValidator) Name() string { return s.name }
func (s *stubValidator) Validate(ctx context.Context, item *domain.SingleRecipientItem) error {
	return s.fn(ctx, item)
}
