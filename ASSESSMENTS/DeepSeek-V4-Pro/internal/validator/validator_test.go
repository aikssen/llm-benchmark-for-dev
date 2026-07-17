package validator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/assessment/notify-service/internal/domain"
)

func TestRecipientValidator(t *testing.T) {
	ctx := context.Background()
	v := NewRecipientValidator()

	tests := []struct {
		name    string
		notif   *domain.Notification
		wantOK  bool
	}{
		{
			name: "valid email",
			notif: &domain.Notification{
				Channel:    domain.ChannelEmail,
				Recipients: []domain.Recipient{{ID: "user@test.com"}},
			},
			wantOK: true,
		},
		{
			name: "invalid email format",
			notif: &domain.Notification{
				Channel:    domain.ChannelEmail,
				Recipients: []domain.Recipient{{ID: "notanemail"}},
			},
			wantOK: false,
		},
		{
			name: "valid phone",
			notif: &domain.Notification{
				Channel:    domain.ChannelSMS,
				Recipients: []domain.Recipient{{ID: "1234567890"}},
			},
			wantOK: true,
		},
		{
			name: "invalid phone",
			notif: &domain.Notification{
				Channel:    domain.ChannelSMS,
				Recipients: []domain.Recipient{{ID: "123"}},
			},
			wantOK: false,
		},
		{
			name: "valid push token",
			notif: &domain.Notification{
				Channel:    domain.ChannelPush,
				Recipients: []domain.Recipient{{ID: "device-token-123"}},
			},
			wantOK: true,
		},
		{
			name: "empty push token",
			notif: &domain.Notification{
				Channel:    domain.ChannelPush,
				Recipients: []domain.Recipient{{ID: ""}},
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := v.Validate(ctx, tt.notif)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Passed != tt.wantOK {
				t.Errorf("expected passed=%v, got passed=%v", tt.wantOK, result.Passed)
			}
		})
	}
}

func TestPreferencesValidator(t *testing.T) {
	ctx := context.Background()
	v := NewPreferencesValidator()

	t.Run("blocked recipient", func(t *testing.T) {
		n := &domain.Notification{
			Channel:    domain.ChannelEmail,
			Recipients: []domain.Recipient{{ID: "blocked@example.com"}},
		}
		result, err := v.Validate(ctx, n)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed {
			t.Error("expected validation to fail for blocked user")
		}
	})

	t.Run("allowed recipient", func(t *testing.T) {
		n := &domain.Notification{
			Channel:    domain.ChannelEmail,
			Recipients: []domain.Recipient{{ID: "user@test.com"}},
		}
		result, err := v.Validate(ctx, n)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Passed {
			t.Error("expected validation to pass for allowed user")
		}
	})
}

func TestRateLimitValidator(t *testing.T) {
	ctx := context.Background()
	v := NewRateLimitValidator(2, 10*time.Second)

	n := &domain.Notification{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "user@test.com"}},
	}

	for i := 0; i < 3; i++ {
		result, err := v.Validate(ctx, n)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if i < 2 {
			if !result.Passed {
				t.Errorf("attempt %d: expected pass, got fail", i+1)
			}
		} else {
			if result.Passed {
				t.Error("expected rate limit to be exceeded after 3 requests")
			}
		}
	}
}

func TestContentValidator(t *testing.T) {
	ctx := context.Background()
	v := NewContentValidator()

	t.Run("valid content", func(t *testing.T) {
		n := &domain.Notification{
			Channel: domain.ChannelEmail,
			Message: domain.Message{Body: "Hello", Subject: "Greetings"},
		}
		result, err := v.Validate(ctx, n)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Passed {
			t.Error("expected validation to pass")
		}
	})

	t.Run("oversized content", func(t *testing.T) {
		n := &domain.Notification{
			Channel: domain.ChannelEmail,
			Message: domain.Message{Body: strings.Repeat("a", 10001)},
		}
		result, err := v.Validate(ctx, n)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed {
			t.Error("expected validation to fail for oversized message")
		}
	})

	t.Run("subject on SMS", func(t *testing.T) {
		n := &domain.Notification{
			Channel: domain.ChannelSMS,
			Message: domain.Message{Body: "Hello", Subject: "Hi"},
		}
		result, err := v.Validate(ctx, n)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed {
			t.Error("expected validation to fail for subject on SMS")
		}
	})
}
