package validation

import (
	"context"
	"strings"
	"testing"
	"time"

	"notificationservice/internal/domain"
)

func TestRecipientValidator(t *testing.T) {
	tests := []struct {
		name      string
		channel   domain.Channel
		recipient string
		wantErr   bool
	}{
		{"valid email", domain.ChannelEmail, "user@example.com", false},
		{"email with display name", domain.ChannelEmail, "User <user@example.com>", false},
		{"email missing domain", domain.ChannelEmail, "user@", true},
		{"email plain text", domain.ChannelEmail, "not-an-email", true},
		{"valid e164", domain.ChannelSMS, "+14155552671", false},
		{"phone without plus", domain.ChannelSMS, "14155552671", true},
		{"phone with leading zero", domain.ChannelSMS, "+04155552671", true},
		{"phone too short", domain.ChannelSMS, "+1415", true},
		{"phone with letters", domain.ChannelSMS, "+1415555ABCD", true},
		{"valid push token", domain.ChannelPush, "fcm-token_0123456789abcdef:APA91", false},
		{"push token too short", domain.ChannelPush, "short", true},
		{"push token invalid chars", domain.ChannelPush, "token with spaces and more", true},
		{"unknown channel", domain.Channel("fax"), "whatever", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Recipient{}.Validate(context.Background(), Input{Channel: tt.channel, Recipient: tt.recipient})
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q, %q) error = %v, wantErr %v", tt.channel, tt.recipient, err, tt.wantErr)
			}
		})
	}
}

func TestContentValidator(t *testing.T) {
	tests := []struct {
		name    string
		channel domain.Channel
		msg     domain.Message
		wantErr bool
	}{
		{"valid email", domain.ChannelEmail, domain.Message{Subject: "hi", Body: "hello"}, false},
		{"email without subject", domain.ChannelEmail, domain.Message{Body: "hello"}, true},
		{"empty body", domain.ChannelSMS, domain.Message{}, true},
		{"valid sms", domain.ChannelSMS, domain.Message{Body: "ping"}, false},
		{"sms too long", domain.ChannelSMS, domain.Message{Body: strings.Repeat("x", 1601)}, true},
		{"push too large", domain.ChannelPush, domain.Message{Body: strings.Repeat("x", 4097)}, true},
		{"subject too long", domain.ChannelPush, domain.Message{Subject: strings.Repeat("s", 257), Body: "b"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Content{}.Validate(context.Background(), Input{Channel: tt.channel, Message: tt.msg})
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPreferencesValidator(t *testing.T) {
	prefs := NewStaticPreferences(0,
		"optout@example.com", // opted out of everything
		"sms:+14155552671",   // opted out of SMS only
	)
	v := Preferences{Checker: prefs}

	cases := []struct {
		recipient string
		channel   domain.Channel
		wantErr   bool
	}{
		{"user@example.com", domain.ChannelEmail, false},
		{"optout@example.com", domain.ChannelEmail, true},
		{"+14155552671", domain.ChannelSMS, true},
		{"+14155552671", domain.ChannelPush, false},
	}
	for _, c := range cases {
		err := v.Validate(context.Background(), Input{Channel: c.channel, Recipient: c.recipient})
		if (err != nil) != c.wantErr {
			t.Errorf("Validate(%s, %s) error = %v, wantErr %v", c.channel, c.recipient, err, c.wantErr)
		}
	}
}

func TestStaticPreferencesHonorsContext(t *testing.T) {
	prefs := NewStaticPreferences(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prefs.Allows(ctx, "user@example.com", domain.ChannelEmail); err == nil {
		t.Error("expected context error from cancelled lookup")
	}
}

func TestRateLimit(t *testing.T) {
	rl := NewRateLimit(2, time.Minute)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }

	in := Input{Channel: domain.ChannelEmail, Recipient: "user@example.com"}
	other := Input{Channel: domain.ChannelEmail, Recipient: "other@example.com"}

	for i := range 2 {
		if err := rl.Validate(context.Background(), in); err != nil {
			t.Fatalf("request %d should pass: %v", i+1, err)
		}
	}
	if err := rl.Validate(context.Background(), in); err == nil {
		t.Fatal("third request within the window should be rejected")
	}
	// Other recipients are unaffected.
	if err := rl.Validate(context.Background(), other); err != nil {
		t.Fatalf("different recipient should pass: %v", err)
	}
	// After the window slides, the recipient is allowed again.
	now = now.Add(61 * time.Second)
	if err := rl.Validate(context.Background(), in); err != nil {
		t.Fatalf("request after window should pass: %v", err)
	}
}
