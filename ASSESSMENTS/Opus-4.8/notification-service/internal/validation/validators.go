package validation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

// --- Recipient validation ----------------------------------------------------

// e164 is a permissive E.164 phone-number matcher (optional leading +, 8-15
// digits). It is deliberately simple; real deployments would use a library such
// as libphonenumber.
var e164 = regexp.MustCompile(`^\+?[1-9]\d{7,14}$`)

// RecipientValidator checks that the recipient address is well-formed for its
// channel. The rules are channel-specific, which is why this cannot be a single
// generic regex.
type RecipientValidator struct{}

func (RecipientValidator) Name() string { return "recipient" }

func (RecipientValidator) Validate(_ context.Context, n *domain.Notification) error {
	r := strings.TrimSpace(n.Recipient)
	if r == "" {
		return errors.New("recipient is empty")
	}
	switch n.Channel {
	case domain.ChannelEmail:
		if _, err := mail.ParseAddress(r); err != nil {
			return fmt.Errorf("invalid email address: %v", err)
		}
	case domain.ChannelSMS:
		if !e164.MatchString(r) {
			return errors.New("invalid phone number, expected E.164 format")
		}
	case domain.ChannelPush:
		// Push tokens are opaque provider-issued strings; we only sanity-check
		// length to catch obviously broken input.
		if len(r) < 8 {
			return errors.New("invalid push token")
		}
	}
	return nil
}

// --- Content validation ------------------------------------------------------

// maxBodyBytes bounds the message size per channel. SMS is intentionally small
// to reflect real carrier limits.
var maxBodyBytes = map[domain.Channel]int{
	domain.ChannelEmail: 100_000,
	domain.ChannelSMS:   1_600, // ~10 concatenated segments
	domain.ChannelPush:  4_000,
}

// ContentValidator checks the message body is present and within channel limits.
type ContentValidator struct{}

func (ContentValidator) Name() string { return "content" }

func (ContentValidator) Validate(_ context.Context, n *domain.Notification) error {
	if strings.TrimSpace(n.Message.Body) == "" {
		return errors.New("message body is empty")
	}
	if limit, ok := maxBodyBytes[n.Channel]; ok && len(n.Message.Body) > limit {
		return fmt.Errorf("message body exceeds %d bytes for channel %s", limit, n.Channel)
	}
	return nil
}

// --- User preferences --------------------------------------------------------

// PreferenceStore reports whether a recipient accepts a given channel. Modeling
// it as an interface lets the real implementation live behind a database or a
// remote preferences service without changing the validator.
type PreferenceStore interface {
	Accepts(ctx context.Context, recipient string, channel domain.Channel) (bool, error)
}

// PreferenceValidator rejects notifications to recipients who have opted out of
// the channel.
type PreferenceValidator struct {
	Store PreferenceStore
}

func (PreferenceValidator) Name() string { return "preferences" }

func (v PreferenceValidator) Validate(ctx context.Context, n *domain.Notification) error {
	ok, err := v.Store.Accepts(ctx, n.Recipient, n.Channel)
	if err != nil {
		return fmt.Errorf("could not check preferences: %w", err)
	}
	if !ok {
		return fmt.Errorf("recipient has opted out of %s notifications", n.Channel)
	}
	return nil
}

// InMemoryPreferences is a simple opt-out set for local use: any (recipient,
// channel) pair added via OptOut is rejected; everything else is accepted.
type InMemoryPreferences struct {
	mu       sync.RWMutex
	optedOut map[string]struct{}
}

func NewInMemoryPreferences() *InMemoryPreferences {
	return &InMemoryPreferences{optedOut: make(map[string]struct{})}
}

func prefKey(recipient string, c domain.Channel) string {
	return string(c) + "|" + recipient
}

func (p *InMemoryPreferences) OptOut(recipient string, c domain.Channel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.optedOut[prefKey(recipient, c)] = struct{}{}
}

func (p *InMemoryPreferences) Accepts(_ context.Context, recipient string, c domain.Channel) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, blocked := p.optedOut[prefKey(recipient, c)]
	return !blocked, nil
}

// --- Rate limiting -----------------------------------------------------------

// RateLimiter decides whether another notification may be sent to a recipient
// right now. Kept as an interface so a distributed limiter (e.g. Redis) can
// replace the in-memory one in production.
type RateLimiter interface {
	Allow(recipient string, channel domain.Channel) bool
}

// RateLimitValidator rejects notifications that exceed the configured rate.
type RateLimitValidator struct {
	Limiter RateLimiter
}

func (RateLimitValidator) Name() string { return "rate_limit" }

func (v RateLimitValidator) Validate(_ context.Context, n *domain.Notification) error {
	if !v.Limiter.Allow(n.Recipient, n.Channel) {
		return errors.New("rate limit exceeded for recipient")
	}
	return nil
}

// FixedWindowLimiter is a per-(recipient,channel) fixed-window rate limiter. It
// is intentionally minimal but concurrency-safe; a production system would more
// likely use a sliding window or token bucket in a shared store.
type FixedWindowLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time // injectable clock for deterministic tests

	mu      sync.Mutex
	windows map[string]*windowCounter
}

type windowCounter struct {
	start time.Time
	count int
}

// NewFixedWindowLimiter allows at most limit events per window per recipient and
// channel. A non-positive limit disables limiting (always allows).
func NewFixedWindowLimiter(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		windows: make(map[string]*windowCounter),
	}
}

func (l *FixedWindowLimiter) Allow(recipient string, channel domain.Channel) bool {
	if l.limit <= 0 {
		return true
	}
	key := prefKey(recipient, channel)
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.windows[key] = &windowCounter{start: now, count: 1}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}
