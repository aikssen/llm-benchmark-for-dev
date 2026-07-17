package validation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

func notif(ch domain.Channel, recipient, body string) *domain.Notification {
	return &domain.Notification{Channel: ch, Recipient: recipient, Message: domain.Message{Body: body}}
}

func TestRecipientValidator(t *testing.T) {
	v := RecipientValidator{}
	cases := []struct {
		name    string
		n       *domain.Notification
		wantErr bool
	}{
		{"valid email", notif(domain.ChannelEmail, "user@example.com", "hi"), false},
		{"invalid email", notif(domain.ChannelEmail, "not-an-email", "hi"), true},
		{"empty recipient", notif(domain.ChannelEmail, "  ", "hi"), true},
		{"valid sms", notif(domain.ChannelSMS, "+14155552671", "hi"), false},
		{"invalid sms", notif(domain.ChannelSMS, "12", "hi"), true},
		{"valid push", notif(domain.ChannelPush, "a-valid-token-123", "hi"), false},
		{"short push", notif(domain.ChannelPush, "short", "hi"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(context.Background(), tc.n)
			if (err != nil) != tc.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestContentValidator(t *testing.T) {
	v := ContentValidator{}
	if err := v.Validate(context.Background(), notif(domain.ChannelEmail, "u@e.com", "")); err == nil {
		t.Error("empty body should fail")
	}
	long := strings.Repeat("x", maxBodyBytes[domain.ChannelSMS]+1)
	if err := v.Validate(context.Background(), notif(domain.ChannelSMS, "+14155552671", long)); err == nil {
		t.Error("over-limit SMS body should fail")
	}
	if err := v.Validate(context.Background(), notif(domain.ChannelEmail, "u@e.com", "hello")); err != nil {
		t.Errorf("valid content should pass, got %v", err)
	}
}

func TestPreferenceValidator(t *testing.T) {
	prefs := NewInMemoryPreferences()
	prefs.OptOut("user@example.com", domain.ChannelEmail)
	v := PreferenceValidator{Store: prefs}

	if err := v.Validate(context.Background(), notif(domain.ChannelEmail, "user@example.com", "hi")); err == nil {
		t.Error("opted-out recipient should be rejected")
	}
	if err := v.Validate(context.Background(), notif(domain.ChannelSMS, "user@example.com", "hi")); err != nil {
		t.Errorf("other channel should still be accepted, got %v", err)
	}
}

func TestFixedWindowLimiter(t *testing.T) {
	// Freeze time so the test is deterministic.
	now := time.Unix(0, 0)
	l := NewFixedWindowLimiter(2, time.Minute)
	l.now = func() time.Time { return now }

	rcpt, ch := "user@example.com", domain.ChannelEmail
	if !l.Allow(rcpt, ch) {
		t.Fatal("first call within limit should be allowed")
	}
	if !l.Allow(rcpt, ch) {
		t.Fatal("second call within limit should be allowed")
	}
	if l.Allow(rcpt, ch) {
		t.Error("third call in the same window should be blocked")
	}

	// Advance past the window; the counter resets.
	now = now.Add(time.Minute + time.Second)
	if !l.Allow(rcpt, ch) {
		t.Error("call in a new window should be allowed")
	}
}

func TestFixedWindowLimiter_DisabledWhenLimitZero(t *testing.T) {
	l := NewFixedWindowLimiter(0, time.Minute)
	for range 100 {
		if !l.Allow("x", domain.ChannelEmail) {
			t.Fatal("limiter with limit 0 should always allow")
		}
	}
}
