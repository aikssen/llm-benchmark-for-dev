package validation

import (
	"context"
	"testing"
	"time"

	"notifications/internal/domain"
	"notifications/internal/preferences"
	"notifications/internal/ratelimit"
)

func notif(channel domain.Channel, recipient string, content domain.Content) *domain.Notification {
	return domain.New("req-test", channel, recipient, content, time.Now())
}

func TestRecipientValidator(t *testing.T) {
	cases := []struct {
		name      string
		channel   domain.Channel
		recipient string
		wantErr   bool
	}{
		{"valid email", domain.ChannelEmail, "user@example.com", false},
		{"invalid email no at", domain.ChannelEmail, "userexample.com", true},
		{"invalid email no domain dot", domain.ChannelEmail, "user@example", true},
		{"empty email", domain.ChannelEmail, "", true},
		{"valid phone", domain.ChannelSMS, "+15551234567", false},
		{"phone without plus", domain.ChannelSMS, "15551234567", true},
		{"phone too short", domain.ChannelSMS, "+1555", true},
		{"phone with letters", domain.ChannelSMS, "+1555ABCDEF", true},
		{"valid token", domain.ChannelPush, "abcdef1234567890ABCDEF", false},
		{"token too short", domain.ChannelPush, "short", true},
		{"unsupported channel", domain.Channel("carrier-pigeon"), "anything", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RecipientValidator{}.Validate(context.Background(), notif(tc.channel, tc.recipient, domain.Content{Body: "hi"}))
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestContentValidator(t *testing.T) {
	long := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		return string(b)
	}
	cases := []struct {
		name    string
		channel domain.Channel
		content domain.Content
		wantErr bool
	}{
		{"email ok", domain.ChannelEmail, domain.Content{Subject: "Hi", Body: "body"}, false},
		{"email missing subject", domain.ChannelEmail, domain.Content{Body: "body"}, true},
		{"email empty body", domain.ChannelEmail, domain.Content{Subject: "Hi"}, true},
		{"sms ok", domain.ChannelSMS, domain.Content{Body: "hello"}, false},
		{"sms too long", domain.ChannelSMS, domain.Content{Body: long(1601)}, true},
		{"push ok", domain.ChannelPush, domain.Content{Body: "hello"}, false},
		{"push too long", domain.ChannelPush, domain.Content{Body: long(241)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ContentValidator{}.Validate(context.Background(), notif(tc.channel, "whatever-recipient", tc.content))
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestPreferencesValidator(t *testing.T) {
	v := PreferencesValidator{Store: preferences.NewSimulated()}
	cases := []struct {
		recipient string
		channel   domain.Channel
		wantErr   bool
	}{
		{"user@example.com", domain.ChannelEmail, false},
		{"optout@example.com", domain.ChannelEmail, true},
		{"user+optout-sms@example.com", domain.ChannelSMS, true},
		{"user+optout-sms@example.com", domain.ChannelEmail, false}, // opted out of sms only
		{"+15551234567", domain.ChannelSMS, false},
	}
	for _, tc := range cases {
		err := v.Validate(context.Background(), notif(tc.channel, tc.recipient, domain.Content{Body: "hi"}))
		if (err != nil) != tc.wantErr {
			t.Fatalf("recipient=%s channel=%s: got err=%v, wantErr=%v", tc.recipient, tc.channel, err, tc.wantErr)
		}
	}
}

func TestRateLimitValidator(t *testing.T) {
	v := RateLimitValidator{Limiter: ratelimit.New(2, time.Minute)}
	n := notif(domain.ChannelEmail, "user@example.com", domain.Content{Body: "hi"})

	if err := v.Validate(context.Background(), n); err != nil {
		t.Fatalf("first call should pass: %v", err)
	}
	if err := v.Validate(context.Background(), n); err != nil {
		t.Fatalf("second call should pass: %v", err)
	}
	if err := v.Validate(context.Background(), n); err == nil {
		t.Fatal("third call should be rate limited")
	}

	// A different recipient has its own bucket.
	other := notif(domain.ChannelEmail, "other@example.com", domain.Content{Body: "hi"})
	if err := v.Validate(context.Background(), other); err != nil {
		t.Fatalf("different recipient should not be limited: %v", err)
	}
}
