package validation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
)

func TestRecipientValidator(t *testing.T) {
	v := NewRecipientValidator()
	ctx := context.Background()

	cases := []struct {
		name    string
		n       *domain.Notification
		wantErr bool
	}{
		{"valid email", &domain.Notification{Recipient: "a@b.com", Channel: domain.ChannelEmail}, false},
		{"invalid email", &domain.Notification{Recipient: "not-an-email", Channel: domain.ChannelEmail}, true},
		{"valid sms", &domain.Notification{Recipient: "+15551234567", Channel: domain.ChannelSMS}, false},
		{"invalid sms", &domain.Notification{Recipient: "5551234", Channel: domain.ChannelSMS}, true},
		{"valid push", &domain.Notification{Recipient: "device-token-abc", Channel: domain.ChannelPush}, false},
		{"empty", &domain.Notification{Recipient: "", Channel: domain.ChannelEmail}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(ctx, tc.n)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestContentValidator(t *testing.T) {
	v := NewContentValidator(100)
	ctx := context.Background()

	err := v.Validate(ctx, &domain.Notification{
		Channel: domain.ChannelEmail,
		Body:    "hello",
		Subject: "",
	})
	if err == nil {
		t.Fatal("expected subject required for email")
	}

	err = v.Validate(ctx, &domain.Notification{
		Channel: domain.ChannelSMS,
		Body:    string(make([]byte, 161)),
	})
	if err == nil {
		t.Fatal("expected SMS body too long")
	}
}

func TestPipelineShortCircuits(t *testing.T) {
	first := &countingValidator{name: "first", err: errors.New("boom")}
	second := &countingValidator{name: "second"}
	p := NewPipeline(first, second)

	err := p.Validate(context.Background(), &domain.Notification{Body: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("short-circuit failed: first=%d second=%d", first.calls, second.calls)
	}
}

func TestPreferencesValidator(t *testing.T) {
	prefs := NewMemoryPreferences()
	prefs.OptOut("user@example.com", domain.ChannelEmail)
	v := NewPreferencesValidator(prefs)

	err := v.Validate(context.Background(), &domain.Notification{
		Recipient: "user@example.com",
		Channel:   domain.ChannelEmail,
	})
	if err == nil {
		t.Fatal("expected opt-out rejection")
	}

	err = v.Validate(context.Background(), &domain.Notification{
		Recipient: "user@example.com",
		Channel:   domain.ChannelSMS,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimitValidator(t *testing.T) {
	limiter := NewSlidingWindowLimiter(2, time.Minute)
	v := NewRateLimitValidator(limiter)
	n := &domain.Notification{Recipient: "a@b.com", Channel: domain.ChannelEmail}

	if err := v.Validate(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(context.Background(), n); err == nil {
		t.Fatal("expected rate limit exceeded")
	}
}

type countingValidator struct {
	name  string
	err   error
	calls int
}

func (c *countingValidator) Name() string { return c.name }

func (c *countingValidator) Validate(context.Context, *domain.Notification) error {
	c.calls++
	return c.err
}
