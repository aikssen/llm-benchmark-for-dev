package validation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"notifications/internal/domain"
	"notifications/internal/preferences"
	"notifications/internal/ratelimit"
)

// ---- Recipient format -------------------------------------------------

var (
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneRe = regexp.MustCompile(`^\+[1-9]\d{7,14}$`) // E.164
	tokenRe = regexp.MustCompile(`^[A-Za-z0-9_:\-]{16,512}$`)
)

// RecipientValidator checks that the recipient address has a valid shape
// for the notification channel.
type RecipientValidator struct{}

func (RecipientValidator) Name() string { return "recipient" }

func (RecipientValidator) Validate(_ context.Context, n *domain.Notification) error {
	switch n.Channel {
	case domain.ChannelEmail:
		if !emailRe.MatchString(n.Recipient) {
			return fmt.Errorf("invalid email address %q", n.Recipient)
		}
	case domain.ChannelSMS:
		if !phoneRe.MatchString(n.Recipient) {
			return fmt.Errorf("invalid phone number %q (expected E.164, e.g. +15551234567)", n.Recipient)
		}
	case domain.ChannelPush:
		if !tokenRe.MatchString(n.Recipient) {
			return fmt.Errorf("invalid device token %q", n.Recipient)
		}
	default:
		return fmt.Errorf("unsupported channel %q", n.Channel)
	}
	return nil
}

// ---- Content ----------------------------------------------------------

// Per-channel message constraints.
const (
	maxSubjectLen   = 200
	maxEmailBodyLen = 100_000
	maxSMSBodyLen   = 1600 // concatenated SMS
	maxPushBodyLen  = 240
	maxPushTitleLen = 65
)

// ContentValidator enforces per-channel message rules.
type ContentValidator struct{}

func (ContentValidator) Name() string { return "content" }

func (ContentValidator) Validate(_ context.Context, n *domain.Notification) error {
	if strings.TrimSpace(n.Content.Body) == "" {
		return errors.New("message body must not be empty")
	}
	switch n.Channel {
	case domain.ChannelEmail:
		if strings.TrimSpace(n.Content.Subject) == "" {
			return errors.New("email notifications require a subject")
		}
		if len(n.Content.Subject) > maxSubjectLen {
			return fmt.Errorf("subject exceeds %d characters", maxSubjectLen)
		}
		if len(n.Content.Body) > maxEmailBodyLen {
			return fmt.Errorf("body exceeds %d characters", maxEmailBodyLen)
		}
	case domain.ChannelSMS:
		if len(n.Content.Body) > maxSMSBodyLen {
			return fmt.Errorf("SMS body exceeds %d characters", maxSMSBodyLen)
		}
	case domain.ChannelPush:
		if len(n.Content.Subject) > maxPushTitleLen {
			return fmt.Errorf("push title exceeds %d characters", maxPushTitleLen)
		}
		if len(n.Content.Body) > maxPushBodyLen {
			return fmt.Errorf("push body exceeds %d characters", maxPushBodyLen)
		}
	default:
		return fmt.Errorf("unsupported channel %q", n.Channel)
	}
	return nil
}

// ---- User preferences ---------------------------------------------------

// PreferencesValidator rejects notifications for recipients who opted out
// of the channel. The preferences lookup may involve I/O (a remote
// service in production); Latency simulates that cost and demonstrates
// that the pipeline cancels slow checks when another validator fails.
type PreferencesValidator struct {
	Store   preferences.Store
	Latency time.Duration // simulated lookup latency; 0 disables
}

func (PreferencesValidator) Name() string { return "preferences" }

func (v PreferencesValidator) Validate(ctx context.Context, n *domain.Notification) error {
	if v.Latency > 0 {
		if err := sleep(ctx, v.Latency); err != nil {
			return err
		}
	}
	optedOut, err := v.Store.OptedOut(ctx, n.Recipient, n.Channel)
	if err != nil {
		return fmt.Errorf("preferences lookup failed: %w", err)
	}
	if optedOut {
		return fmt.Errorf("recipient opted out of %s notifications", n.Channel)
	}
	return nil
}

// ---- Rate limiting ------------------------------------------------------

// RateLimitValidator caps how many notifications a single recipient may
// receive per channel and time window.
type RateLimitValidator struct {
	Limiter *ratelimit.Limiter
}

func (RateLimitValidator) Name() string { return "rate_limit" }

func (v RateLimitValidator) Validate(_ context.Context, n *domain.Notification) error {
	key := string(n.Channel) + ":" + n.Recipient
	if !v.Limiter.Allow(key) {
		return fmt.Errorf("rate limit exceeded for recipient on channel %s", n.Channel)
	}
	return nil
}

// sleep waits for d or returns early when ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
