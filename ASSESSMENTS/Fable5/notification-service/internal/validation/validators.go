package validation

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"sync"
	"time"

	"notificationservice/internal/domain"
)

// ---------------------------------------------------------------------------
// Recipient format
// ---------------------------------------------------------------------------

var (
	// E.164: leading +, 8 to 15 digits, no leading zero.
	e164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	// Opaque device tokens as issued by push platforms (APNs/FCM-like).
	pushTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_:.\-]{16,512}$`)
)

// Recipient checks that the recipient address is well-formed for the target
// channel.
type Recipient struct{}

func (Recipient) Name() string { return "recipient" }

func (Recipient) Validate(_ context.Context, in Input) error {
	switch in.Channel {
	case domain.ChannelEmail:
		if _, err := mail.ParseAddress(in.Recipient); err != nil {
			return fmt.Errorf("invalid email address %q", in.Recipient)
		}
	case domain.ChannelSMS:
		if !e164Pattern.MatchString(in.Recipient) {
			return fmt.Errorf("invalid phone number %q: must be E.164 (e.g. +14155552671)", in.Recipient)
		}
	case domain.ChannelPush:
		if !pushTokenPattern.MatchString(in.Recipient) {
			return fmt.Errorf("invalid device token")
		}
	default:
		return fmt.Errorf("unsupported channel %q", in.Channel)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Message content
// ---------------------------------------------------------------------------

// Per-channel content limits. Values approximate real provider constraints
// (e.g. SMS concatenation limits, push payload size).
const (
	maxSMSBodyChars   = 1600
	maxPushBodyBytes  = 4 * 1024
	maxEmailBodyBytes = 100 * 1024
	maxSubjectChars   = 256
)

// Content checks channel-specific message rules (required fields, size caps).
type Content struct{}

func (Content) Name() string { return "content" }

func (Content) Validate(_ context.Context, in Input) error {
	if in.Message.Body == "" {
		return fmt.Errorf("message body must not be empty")
	}
	if len(in.Message.Subject) > maxSubjectChars {
		return fmt.Errorf("subject exceeds %d characters", maxSubjectChars)
	}
	switch in.Channel {
	case domain.ChannelEmail:
		if in.Message.Subject == "" {
			return fmt.Errorf("email requires a subject")
		}
		if len(in.Message.Body) > maxEmailBodyBytes {
			return fmt.Errorf("email body exceeds %d bytes", maxEmailBodyBytes)
		}
	case domain.ChannelSMS:
		if n := len([]rune(in.Message.Body)); n > maxSMSBodyChars {
			return fmt.Errorf("sms body has %d characters, maximum is %d", n, maxSMSBodyChars)
		}
	case domain.ChannelPush:
		if len(in.Message.Body) > maxPushBodyBytes {
			return fmt.Errorf("push body exceeds %d bytes", maxPushBodyBytes)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// User preferences
// ---------------------------------------------------------------------------

// PreferenceChecker answers whether a recipient accepts notifications on a
// channel. In production this would query a preference service or database.
type PreferenceChecker interface {
	Allows(ctx context.Context, recipient string, ch domain.Channel) (bool, error)
}

// Preferences rejects deliveries to recipients who opted out of the channel.
type Preferences struct {
	Checker PreferenceChecker
}

func (Preferences) Name() string { return "preferences" }

func (p Preferences) Validate(ctx context.Context, in Input) error {
	allowed, err := p.Checker.Allows(ctx, in.Recipient, in.Channel)
	if err != nil {
		return fmt.Errorf("checking preferences: %w", err)
	}
	if !allowed {
		return fmt.Errorf("recipient opted out of %s notifications", in.Channel)
	}
	return nil
}

// StaticPreferences is a simulated PreferenceChecker backed by a fixed
// opt-out list. Lookup adds a small artificial latency (honoring ctx) to
// mimic a remote preference service.
type StaticPreferences struct {
	optedOut map[string]struct{} // keyed by "channel:recipient" or "recipient" (all channels)
	latency  time.Duration
}

// NewStaticPreferences builds a checker from opt-out entries. Each entry is
// either "recipient" (opted out of every channel) or "channel:recipient".
func NewStaticPreferences(latency time.Duration, optedOut ...string) *StaticPreferences {
	set := make(map[string]struct{}, len(optedOut))
	for _, entry := range optedOut {
		set[entry] = struct{}{}
	}
	return &StaticPreferences{optedOut: set, latency: latency}
}

func (s *StaticPreferences) Allows(ctx context.Context, recipient string, ch domain.Channel) (bool, error) {
	if s.latency > 0 {
		timer := time.NewTimer(s.latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	if _, out := s.optedOut[recipient]; out {
		return false, nil
	}
	_, out := s.optedOut[string(ch)+":"+recipient]
	return !out, nil
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

// RateLimit enforces a per-recipient, per-channel sliding-window limit so a
// single recipient cannot be flooded. State is in-memory; in a multi-instance
// deployment this would live in a shared store such as Redis.
type RateLimit struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

// NewRateLimit allows at most limit deliveries per (channel, recipient) pair
// within the given window.
func NewRateLimit(limit int, window time.Duration) *RateLimit {
	return &RateLimit{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   make(map[string][]time.Time),
	}
}

func (*RateLimit) Name() string { return "rate_limit" }

func (r *RateLimit) Validate(_ context.Context, in Input) error {
	key := string(in.Channel) + ":" + in.Recipient
	now := r.now()
	cutoff := now.Add(-r.window)

	r.mu.Lock()
	defer r.mu.Unlock()

	recent := r.hits[key][:0]
	for _, t := range r.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= r.limit {
		r.hits[key] = recent
		return fmt.Errorf("rate limit exceeded: %d per %s for this recipient", r.limit, r.window)
	}
	r.hits[key] = append(recent, now)
	return nil
}
