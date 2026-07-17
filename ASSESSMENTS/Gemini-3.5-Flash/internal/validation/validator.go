package validation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"notification-service/internal/domain"
)

// Validator defines the interface for all notification checks
type Validator interface {
	Name() string
	Validate(ctx context.Context, n *domain.Notification) error
}

// RecipientValidator validates that the recipient identifier is in the correct format for the channel
type RecipientValidator struct{}

func (v *RecipientValidator) Name() string { return "recipient_format" }

func (v *RecipientValidator) Validate(ctx context.Context, n *domain.Notification) error {
	// Simulate checking (e.g. network resolution or parsing time)
	select {
	case <-time.After(10 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	switch n.Channel {
	case domain.ChannelEmail:
		_, err := mail.ParseAddress(n.Recipient)
		if err != nil {
			return fmt.Errorf("invalid email address format: %w", err)
		}
	case domain.ChannelSMS:
		// Phone numbers should follow E.164, starting with '+' followed by 10-15 digits
		matched, _ := regexp.MatchString(`^\+[1-9]\d{9,14}$`, n.Recipient)
		if !matched {
			return fmt.Errorf("invalid phone number format (expected E.164, e.g., +15551234567)")
		}
	case domain.ChannelPush:
		// Push tokens are usually hex or base64 strings of a certain length
		if len(n.Recipient) < 8 {
			return fmt.Errorf("invalid push notification token format (too short)")
		}
	default:
		return fmt.Errorf("unsupported channel for recipient validation: %s", n.Channel)
	}
	return nil
}

// ContentValidator validates size and presence of notification content
type ContentValidator struct{}

func (v *ContentValidator) Name() string { return "content_limits" }

func (v *ContentValidator) Validate(ctx context.Context, n *domain.Notification) error {
	select {
	case <-time.After(5 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	trimmed := strings.TrimSpace(n.Content)
	if len(trimmed) == 0 {
		return errors.New("notification content cannot be empty")
	}

	switch n.Channel {
	case domain.ChannelSMS:
		if len(trimmed) > 160 {
			return fmt.Errorf("SMS content exceeds limit of 160 characters (got %d)", len(trimmed))
		}
	case domain.ChannelPush:
		if len(trimmed) > 256 {
			return fmt.Errorf("push notification content exceeds limit of 256 characters (got %d)", len(trimmed))
		}
	case domain.ChannelEmail:
		if len(trimmed) > 10000 {
			return fmt.Errorf("email content exceeds limit of 10,000 characters (got %d)", len(trimmed))
		}
	}
	return nil
}

// PreferenceValidator simulates checking user communication settings from a DB
type PreferenceValidator struct{}

func (v *PreferenceValidator) Name() string { return "user_preferences" }

func (v *PreferenceValidator) Validate(ctx context.Context, n *domain.Notification) error {
	// Simulate DB latency
	select {
	case <-time.After(30 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	// We block specific mock recipients for simulation testing
	if strings.Contains(strings.ToLower(n.Recipient), "optout") || strings.Contains(strings.ToLower(n.Recipient), "blocked") {
		return fmt.Errorf("recipient has opted out of %s notifications", n.Channel)
	}
	return nil
}

// RateLimitValidator simulates evaluating rate limits (e.g. against Redis)
type RateLimitValidator struct{}

func (v *RateLimitValidator) Name() string { return "rate_limit" }

func (v *RateLimitValidator) Validate(ctx context.Context, n *domain.Notification) error {
	// Simulate cache lookup latency
	select {
	case <-time.After(20 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	// We trigger simulated rate limits for specific recipients
	if strings.Contains(strings.ToLower(n.Recipient), "ratelimit") || strings.Contains(strings.ToLower(n.Recipient), "spam") {
		return fmt.Errorf("rate limit exceeded for recipient: %s", n.Recipient)
	}
	return nil
}
