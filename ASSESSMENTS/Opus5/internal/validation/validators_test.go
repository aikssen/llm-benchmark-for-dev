package validation_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/preferences"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/provider/simulated"
	"github.com/evercifuentes/notifier/internal/validation"
)

func testRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	r := provider.NewRegistry()
	r.MustRegister(
		simulated.NewEmail(simulated.Behaviour{}),
		simulated.NewSMS(simulated.Behaviour{}),
		simulated.NewPush(simulated.Behaviour{}),
	)
	return r
}

func requireRejection(t *testing.T, err error, wantCode string) *validation.Rejection {
	t.Helper()
	r, ok := validation.AsRejection(err)
	if !ok {
		t.Fatalf("err = %v, want a rejection with code %q", err, wantCode)
	}
	if r.Code != wantCode {
		t.Fatalf("rejection code = %q, want %q (reason: %s)", r.Code, wantCode, r.Reason)
	}
	return r
}

func TestRecipientValidator(t *testing.T) {
	t.Parallel()
	v := validation.NewRecipientValidator(testRegistry(t))

	tests := []struct {
		name     string
		channel  domain.Channel
		address  string
		wantCode string // empty means the delivery is accepted
	}{
		{"valid email", domain.ChannelEmail, "user@example.com", ""},
		{"email with display name", domain.ChannelEmail, "User <user@example.com>", validation.CodeInvalidRecipient},
		{"email without domain", domain.ChannelEmail, "user@", validation.CodeInvalidRecipient},
		{"valid phone", domain.ChannelSMS, "+14155550123", ""},
		{"phone without country code", domain.ChannelSMS, "4155550123", validation.CodeInvalidRecipient},
		{"valid push token", domain.ChannelPush, "abcdefghijklmnopqrstuvwxyz", ""},
		{"push token too short", domain.ChannelPush, "short", validation.CodeInvalidRecipient},
		{"empty address", domain.ChannelEmail, "  ", validation.CodeInvalidRecipient},
		{"unsupported channel", domain.Channel("fax"), "123", validation.CodeUnknownChannel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := v.Validate(context.Background(), validation.Subject{
				Channel:   tt.channel,
				Recipient: domain.Recipient{Address: tt.address},
			})
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			requireRejection(t, err, tt.wantCode)
		})
	}
}

func TestPreferenceValidator(t *testing.T) {
	t.Parallel()

	store := preferences.NewMemoryStore(map[string]preferences.Preferences{
		"opted-out": {OptedOut: map[domain.Channel]bool{domain.ChannelEmail: true}},
		"blocked":   {Blocked: true},
	})
	v := validation.NewPreferenceValidator(store)

	run := func(key string, ch domain.Channel) error {
		return v.Validate(context.Background(), validation.Subject{
			Channel:   ch,
			Recipient: domain.Recipient{ID: key, Address: "user@example.com"},
		})
	}

	t.Run("unknown recipients default to allowed", func(t *testing.T) {
		if err := run("never-seen", domain.ChannelEmail); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})

	t.Run("opt-out is per channel", func(t *testing.T) {
		requireRejection(t, run("opted-out", domain.ChannelEmail), validation.CodeOptedOut)
		if err := run("opted-out", domain.ChannelSMS); err != nil {
			t.Fatalf("opting out of email must not block SMS, got %v", err)
		}
	})

	t.Run("blocked recipients are refused on every channel", func(t *testing.T) {
		requireRejection(t, run("blocked", domain.ChannelEmail), validation.CodeBlocked)
		requireRejection(t, run("blocked", domain.ChannelPush), validation.CodeBlocked)
	})
}

// failingPreferences simulates the preference service being unreachable.
type failingPreferences struct{ err error }

func (f failingPreferences) Get(context.Context, string) (preferences.Preferences, error) {
	return preferences.Preferences{}, f.err
}

func TestPreferenceValidatorDoesNotRejectWhenTheLookupFails(t *testing.T) {
	t.Parallel()

	want := errors.New("connection refused")
	err := validation.NewPreferenceValidator(failingPreferences{err: want}).
		Validate(context.Background(), validation.Subject{
			Channel:   domain.ChannelEmail,
			Recipient: domain.Recipient{ID: "u1"},
		})

	if _, ok := validation.AsRejection(err); ok {
		t.Fatal("an unreachable preference service must not permanently reject the notification")
	}
	if !errors.Is(err, want) {
		t.Fatalf("Validate() = %v, want it to wrap %v", err, want)
	}
}

// stubLimiter allows a fixed number of calls, then refuses.
type stubLimiter struct {
	allowed int
	keys    []string
}

func (s *stubLimiter) Allow(key string) bool {
	s.keys = append(s.keys, key)
	if s.allowed <= 0 {
		return false
	}
	s.allowed--
	return true
}

func TestRateLimitValidator(t *testing.T) {
	t.Parallel()

	limiter := &stubLimiter{allowed: 1}
	v := validation.NewRateLimitValidator(limiter)
	subj := validation.Subject{
		Channel:   domain.ChannelSMS,
		Recipient: domain.Recipient{ID: "u1"},
	}

	if err := v.Validate(context.Background(), subj); err != nil {
		t.Fatalf("first call = %v, want nil", err)
	}
	requireRejection(t, v.Validate(context.Background(), subj), validation.CodeRateLimited)

	// The key must combine channel and recipient so quotas do not leak
	// across channels.
	if got := limiter.keys[0]; got != "sms:u1" {
		t.Errorf("limiter key = %q, want %q", got, "sms:u1")
	}
}

func TestContentValidator(t *testing.T) {
	t.Parallel()

	v := validation.NewContentValidator(validation.DefaultContentLimits(), validation.ContentLimits{MaxBodyLength: 100})

	tests := []struct {
		name     string
		channel  domain.Channel
		message  domain.Message
		wantCode string
	}{
		{"valid email", domain.ChannelEmail, domain.Message{Subject: "Hi", Body: "Hello"}, ""},
		{"email without subject", domain.ChannelEmail, domain.Message{Body: "Hello"}, validation.CodeInvalidContent},
		{"empty body", domain.ChannelSMS, domain.Message{Body: "   "}, validation.CodeInvalidContent},
		{"sms within limit", domain.ChannelSMS, domain.Message{Body: strings.Repeat("a", 1600)}, ""},
		{"sms over limit", domain.ChannelSMS, domain.Message{Body: strings.Repeat("a", 1601)}, validation.CodeInvalidContent},
		{"unknown channel falls back to the default limits", domain.Channel("fax"),
			domain.Message{Body: strings.Repeat("a", 101)}, validation.CodeInvalidContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := v.Validate(context.Background(), validation.Subject{Channel: tt.channel, Message: tt.message})
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			requireRejection(t, err, tt.wantCode)
		})
	}
}

// Limits are expressed in characters, so multi-byte content must not be
// rejected just because it uses more bytes than ASCII would.
func TestContentValidatorCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()

	limits := map[domain.Channel]validation.ContentLimits{
		domain.ChannelSMS: {MaxBodyLength: 10},
	}
	v := validation.NewContentValidator(limits, validation.ContentLimits{})

	// 10 emoji: 10 characters but 40 bytes.
	err := v.Validate(context.Background(), validation.Subject{
		Channel: domain.ChannelSMS,
		Message: domain.Message{Body: strings.Repeat("😀", 10)},
	})
	if err != nil {
		t.Fatalf("Validate() = %v, want nil for a 10-character body", err)
	}
}
