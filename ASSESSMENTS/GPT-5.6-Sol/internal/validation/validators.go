package validation

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"notification/internal/notification"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

type Recipient struct{}

func (Recipient) Name() string { return "recipient" }

func (Recipient) Validate(_ context.Context, channel notification.Channel, recipient string, _ notification.Message) error {
	switch channel {
	case notification.ChannelEmail:
		address, err := mail.ParseAddress(recipient)
		if err != nil || address.Address != recipient {
			return notification.NewValidationError("invalid_recipient", "recipient must be a valid email address")
		}
	case notification.ChannelSMS:
		if !phonePattern.MatchString(recipient) {
			return notification.NewValidationError("invalid_recipient", "recipient must be an E.164 phone number")
		}
	case notification.ChannelPush:
		if len(recipient) < 8 || len(recipient) > 512 || strings.ContainsAny(recipient, " \t\r\n") {
			return notification.NewValidationError("invalid_recipient", "recipient must be a valid push token")
		}
	}
	return nil
}

type Message struct {
	MaxSubjectBytes int
}

func (Message) Name() string { return "message" }

func (v Message) Validate(_ context.Context, channel notification.Channel, _ string, message notification.Message) error {
	if len([]byte(message.Subject)) > v.MaxSubjectBytes {
		return notification.NewValidationError("invalid_message", fmt.Sprintf("subject must be at most %d bytes", v.MaxSubjectBytes))
	}
	if channel == notification.ChannelEmail && strings.TrimSpace(message.Subject) == "" {
		return notification.NewValidationError("invalid_message", "email subject must not be empty")
	}
	return nil
}

type Preferences struct {
	blocked map[string]struct{}
}

func NewPreferences(blocked []string) Preferences {
	values := make(map[string]struct{}, len(blocked))
	for _, recipient := range blocked {
		if recipient = strings.TrimSpace(recipient); recipient != "" {
			values[recipient] = struct{}{}
		}
	}
	return Preferences{blocked: values}
}

func (Preferences) Name() string { return "preferences" }

func (v Preferences) Validate(_ context.Context, _ notification.Channel, recipient string, _ notification.Message) error {
	if _, blocked := v.blocked[recipient]; blocked {
		return notification.NewValidationError("recipient_opted_out", "recipient has opted out")
	}
	return nil
}

type rateWindow struct {
	started time.Time
	count   int
}

type RateLimit struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]rateWindow
	now     func() time.Time
}

func NewRateLimit(limit int, window time.Duration) *RateLimit {
	return &RateLimit{limit: limit, window: window, entries: make(map[string]rateWindow), now: time.Now}
}

func (*RateLimit) Name() string { return "rate_limit" }

func (v *RateLimit) Validate(_ context.Context, channel notification.Channel, recipient string, _ notification.Message) error {
	now := v.now()
	key := string(channel) + ":" + recipient
	v.mu.Lock()
	defer v.mu.Unlock()
	entry := v.entries[key]
	if entry.started.IsZero() || now.Sub(entry.started) >= v.window {
		v.entries[key] = rateWindow{started: now, count: 1}
		return nil
	}
	if entry.count >= v.limit {
		return notification.NewValidationError("rate_limit_exceeded", "recipient rate limit exceeded")
	}
	entry.count++
	v.entries[key] = entry
	return nil
}
