package validation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"notification-service/internal/domain"
)

type Validator interface {
	Name() string
	Validate(ctx context.Context, notification domain.Notification) error
}

type FuncValidator struct {
	ValidatorName string
	Fn            func(ctx context.Context, notification domain.Notification) error
}

func (v FuncValidator) Name() string {
	return v.ValidatorName
}

func (v FuncValidator) Validate(ctx context.Context, notification domain.Notification) error {
	return v.Fn(ctx, notification)
}

type RecipientValidator struct{}

func (RecipientValidator) Name() string {
	return "recipient"
}

func (RecipientValidator) Validate(ctx context.Context, notification domain.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	recipient := strings.TrimSpace(notification.Recipient)
	if recipient == "" {
		return errors.New("recipient is required")
	}

	switch notification.Channel {
	case domain.ChannelEmail:
		if !validEmail(recipient) {
			return errors.New("recipient must be a valid email address")
		}
	case domain.ChannelSMS:
		if !validPhone(recipient) {
			return errors.New("recipient must be an E.164-like phone number")
		}
	case domain.ChannelPush:
		if len(recipient) < 8 || strings.ContainsFunc(recipient, unicode.IsSpace) {
			return errors.New("recipient must be a push token with at least 8 non-space characters")
		}
	default:
		return fmt.Errorf("unsupported channel %q", notification.Channel)
	}

	return nil
}

type MessageValidator struct {
	MaxBodyBytes int
}

func (MessageValidator) Name() string {
	return "message"
}

func (v MessageValidator) Validate(ctx context.Context, notification domain.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	maxBodyBytes := v.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 4096
	}

	if strings.TrimSpace(notification.Message.Body) == "" {
		return errors.New("message body is required")
	}
	if len([]byte(notification.Message.Body)) > maxBodyBytes {
		return fmt.Errorf("message body exceeds %d bytes", maxBodyBytes)
	}
	if len([]byte(notification.Message.Subject)) > 256 {
		return errors.New("message subject exceeds 256 bytes")
	}
	if len(notification.Message.Data) > 25 {
		return errors.New("message data can contain at most 25 keys")
	}

	return nil
}

type PreferenceValidator struct{}

func (PreferenceValidator) Name() string {
	return "preferences"
}

func (PreferenceValidator) Validate(ctx context.Context, notification domain.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	recipient := strings.ToLower(notification.Recipient)
	if strings.Contains(recipient, "blocked") || strings.Contains(recipient, "unsubscribed") {
		return errors.New("recipient has opted out")
	}
	return nil
}

type RateLimitValidator struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	events map[string][]time.Time
}

func NewRateLimitValidator(max int, window time.Duration) *RateLimitValidator {
	if max <= 0 {
		max = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimitValidator{
		max:    max,
		window: window,
		events: make(map[string][]time.Time),
	}
}

func (*RateLimitValidator) Name() string {
	return "rate_limit"
}

func (v *RateLimitValidator) Validate(ctx context.Context, notification domain.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now().UTC()
	key := string(notification.Channel) + ":" + notification.Recipient
	cutoff := now.Add(-v.window)

	v.mu.Lock()
	defer v.mu.Unlock()

	events := v.events[key]
	kept := events[:0]
	for _, eventTime := range events {
		if eventTime.After(cutoff) {
			kept = append(kept, eventTime)
		}
	}
	if len(kept) >= v.max {
		v.events[key] = kept
		return errors.New("recipient rate limit exceeded")
	}

	kept = append(kept, now)
	v.events[key] = kept
	return nil
}

func validEmail(value string) bool {
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}
	domainPart := value[at+1:]
	return strings.Contains(domainPart, ".") && !strings.ContainsAny(value, " \t\r\n")
}

func validPhone(value string) bool {
	if len(value) < 9 || len(value) > 16 || value[0] != '+' {
		return false
	}
	for _, r := range value[1:] {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
