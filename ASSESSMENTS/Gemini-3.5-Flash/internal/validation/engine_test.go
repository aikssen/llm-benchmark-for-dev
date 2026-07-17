package validation

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"notification-service/internal/domain"
)

type dummyValidator struct {
	name       string
	latency    time.Duration
	shouldFail bool
	called     int32
}

func (d *dummyValidator) Name() string {
	return d.name
}

func (d *dummyValidator) Validate(ctx context.Context, n *domain.Notification) error {
	atomic.AddInt32(&d.called, 1)
	select {
	case <-time.After(d.latency):
		if d.shouldFail {
			return errors.New("failed validation")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestValidationEngine_Success(t *testing.T) {
	v1 := &dummyValidator{name: "val1", latency: 5 * time.Millisecond}
	v2 := &dummyValidator{name: "val2", latency: 10 * time.Millisecond}
	engine := NewEngine(v1, v2)

	n := &domain.Notification{
		ID:        "1",
		Recipient: "test@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Hello",
	}

	err := engine.Validate(context.Background(), n)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if atomic.LoadInt32(&v1.called) != 1 || atomic.LoadInt32(&v2.called) != 1 {
		t.Error("expected both validators to be called")
	}
}

func TestValidationEngine_FailFast(t *testing.T) {
	// v1 will fail very quickly
	v1 := &dummyValidator{name: "val1", latency: 5 * time.Millisecond, shouldFail: true}
	// v2 would succeed but takes much longer
	v2 := &dummyValidator{name: "val2", latency: 100 * time.Millisecond, shouldFail: false}

	engine := NewEngine(v1, v2)
	n := &domain.Notification{
		ID:        "1",
		Recipient: "test@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Hello",
	}

	start := time.Now()
	err := engine.Validate(context.Background(), n)
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error message should mention the failed validator's name
	if !errors.Is(err, err) || err.Error() != "[val1]: failed validation" {
		t.Errorf("expected error from val1, got: %v", err)
	}

	// Because of fail-fast / context cancellation, the validation should finish
	// way before the 100ms latency of v2. Usually it takes 5-10ms.
	if duration >= 50*time.Millisecond {
		t.Errorf("fail-fast failed: validation took too long (%v), meaning v2 was not cancelled early", duration)
	}

	if atomic.LoadInt32(&v1.called) != 1 || atomic.LoadInt32(&v2.called) != 1 {
		t.Error("expected both validators to be scheduled")
	}
}

func TestValidationEngine_RealValidators(t *testing.T) {
	recipientVal := &RecipientValidator{}
	contentVal := &ContentValidator{}
	preferenceVal := &PreferenceValidator{}
	rateLimitVal := &RateLimitValidator{}

	engine := NewEngine(recipientVal, contentVal, preferenceVal, rateLimitVal)

	tests := []struct {
		name        string
		n           *domain.Notification
		expectError bool
		errSubstring string
	}{
		{
			name: "Valid Email",
			n: &domain.Notification{
				Recipient: "valid@example.com",
				Channel:   domain.ChannelEmail,
				Content:   "A valid email content",
			},
			expectError: false,
		},
		{
			name: "Invalid Email Format",
			n: &domain.Notification{
				Recipient: "invalidemail.com",
				Channel:   domain.ChannelEmail,
				Content:   "Valid content",
			},
			expectError: true,
			errSubstring: "recipient_format",
		},
		{
			name: "SMS Too Long",
			n: &domain.Notification{
				Recipient: "+15551234567",
				Channel:   domain.ChannelSMS,
				Content:   string(make([]byte, 161)), // 161 chars
			},
			expectError: true,
			errSubstring: "content_limits",
		},
		{
			name: "Preference Blocked",
			n: &domain.Notification{
				Recipient: "optout@example.com",
				Channel:   domain.ChannelEmail,
				Content:   "Hi",
			},
			expectError: true,
			errSubstring: "user_preferences",
		},
		{
			name: "Rate Limited",
			n: &domain.Notification{
				Recipient: "ratelimit_user@example.com",
				Channel:   domain.ChannelEmail,
				Content:   "Hi",
			},
			expectError: true,
			errSubstring: "rate_limit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := engine.Validate(context.Background(), tc.n)
			if tc.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tc.errSubstring != "" && !strings.Contains(err.Error(), tc.errSubstring) {
					t.Errorf("expected error to contain %q, got: %v", tc.errSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}
