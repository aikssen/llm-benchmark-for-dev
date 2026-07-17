package validation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/evercifuentes/notifications/internal/domain"
)

// ErrValidation is returned when a validation rule fails.
var ErrValidation = errors.New("validation failed")

// Validator checks a single independent rule against a notification.
// Validators must not depend on each other; the pipeline short-circuits on first failure
// to avoid consuming unnecessary resources.
type Validator interface {
	Name() string
	Validate(ctx context.Context, n *domain.Notification) error
}

// Pipeline runs validators sequentially and stops on the first failure.
type Pipeline struct {
	validators []Validator
}

// NewPipeline builds a validation pipeline.
func NewPipeline(validators ...Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

// Validate runs all validators until one fails.
func (p *Pipeline) Validate(ctx context.Context, n *domain.Notification) error {
	for _, v := range p.validators {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := v.Validate(ctx, n); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrValidation, v.Name(), err)
		}
	}
	return nil
}

// RecipientValidator validates recipient format per channel.
type RecipientValidator struct {
	emailRE *regexp.Regexp
	phoneRE *regexp.Regexp
}

// NewRecipientValidator creates a recipient format validator.
func NewRecipientValidator() *RecipientValidator {
	return &RecipientValidator{
		emailRE: regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`),
		phoneRE: regexp.MustCompile(`^\+[1-9]\d{7,14}$`),
	}
}

// Name implements Validator.
func (v *RecipientValidator) Name() string { return "recipient" }

// Validate implements Validator.
func (v *RecipientValidator) Validate(_ context.Context, n *domain.Notification) error {
	recipient := strings.TrimSpace(n.Recipient)
	if recipient == "" {
		return errors.New("recipient is required")
	}

	switch n.Channel {
	case domain.ChannelEmail:
		if !v.emailRE.MatchString(recipient) {
			return fmt.Errorf("invalid email recipient %q", recipient)
		}
	case domain.ChannelSMS:
		if !v.phoneRE.MatchString(recipient) {
			return fmt.Errorf("invalid SMS recipient %q (E.164 expected)", recipient)
		}
	case domain.ChannelPush:
		if len(recipient) < 8 {
			return fmt.Errorf("invalid push device token %q", recipient)
		}
	default:
		return fmt.Errorf("unsupported channel %q", n.Channel)
	}
	return nil
}

// ContentValidator validates message contents.
type ContentValidator struct {
	maxBodyRunes int
}

// NewContentValidator creates a content validator.
func NewContentValidator(maxBodyRunes int) *ContentValidator {
	if maxBodyRunes <= 0 {
		maxBodyRunes = 2000
	}
	return &ContentValidator{maxBodyRunes: maxBodyRunes}
}

// Name implements Validator.
func (v *ContentValidator) Name() string { return "content" }

// Validate implements Validator.
func (v *ContentValidator) Validate(_ context.Context, n *domain.Notification) error {
	body := strings.TrimSpace(n.Body)
	if body == "" {
		return errors.New("body is required")
	}
	if utf8.RuneCountInString(body) > v.maxBodyRunes {
		return fmt.Errorf("body exceeds %d characters", v.maxBodyRunes)
	}
	if n.Channel == domain.ChannelEmail && strings.TrimSpace(n.Subject) == "" {
		return errors.New("subject is required for email")
	}
	if n.Channel == domain.ChannelSMS && utf8.RuneCountInString(body) > 160 {
		return errors.New("SMS body exceeds 160 characters")
	}
	return nil
}

// PreferencesStore abstracts user notification preferences.
type PreferencesStore interface {
	// IsChannelAllowed reports whether the recipient accepts the channel.
	IsChannelAllowed(ctx context.Context, recipient string, channel domain.Channel) (bool, error)
}

// MemoryPreferences is an in-memory preferences store.
// By default all channels are allowed unless explicitly opted out.
type MemoryPreferences struct {
	mu   sync.RWMutex
	deny map[string]map[domain.Channel]struct{}
}

// NewMemoryPreferences creates an empty preferences store.
func NewMemoryPreferences() *MemoryPreferences {
	return &MemoryPreferences{deny: make(map[string]map[domain.Channel]struct{})}
}

// OptOut marks a recipient as not accepting a channel.
func (p *MemoryPreferences) OptOut(recipient string, channel domain.Channel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deny[recipient] == nil {
		p.deny[recipient] = make(map[domain.Channel]struct{})
	}
	p.deny[recipient][channel] = struct{}{}
}

// IsChannelAllowed implements PreferencesStore.
func (p *MemoryPreferences) IsChannelAllowed(_ context.Context, recipient string, channel domain.Channel) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if channels, ok := p.deny[recipient]; ok {
		if _, denied := channels[channel]; denied {
			return false, nil
		}
	}
	return true, nil
}

// PreferencesValidator checks user opt-out preferences.
type PreferencesValidator struct {
	prefs PreferencesStore
}

// NewPreferencesValidator creates a preferences validator.
func NewPreferencesValidator(prefs PreferencesStore) *PreferencesValidator {
	return &PreferencesValidator{prefs: prefs}
}

// Name implements Validator.
func (v *PreferencesValidator) Name() string { return "preferences" }

// Validate implements Validator.
func (v *PreferencesValidator) Validate(ctx context.Context, n *domain.Notification) error {
	allowed, err := v.prefs.IsChannelAllowed(ctx, n.Recipient, n.Channel)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("recipient %q opted out of %s", n.Recipient, n.Channel)
	}
	return nil
}

// RateLimiter limits notifications per recipient.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// SlidingWindowLimiter is a simple per-key rate limiter.
type SlidingWindowLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	requests map[string][]time.Time
}

// NewSlidingWindowLimiter creates a rate limiter.
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	return &SlidingWindowLimiter{
		limit:    limit,
		window:   window,
		requests: make(map[string][]time.Time),
	}
}

// Allow reports whether a request for key is within the rate limit.
func (l *SlidingWindowLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	kept := l.requests[key][:0]
	for _, t := range l.requests[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.requests[key] = kept

	if len(kept) >= l.limit {
		return false, nil
	}
	l.requests[key] = append(l.requests[key], now)
	return true, nil
}

// RateLimitValidator enforces per-recipient rate limits.
type RateLimitValidator struct {
	limiter RateLimiter
}

// NewRateLimitValidator creates a rate limit validator.
func NewRateLimitValidator(limiter RateLimiter) *RateLimitValidator {
	return &RateLimitValidator{limiter: limiter}
}

// Name implements Validator.
func (v *RateLimitValidator) Name() string { return "rate_limit" }

// Validate implements Validator.
func (v *RateLimitValidator) Validate(ctx context.Context, n *domain.Notification) error {
	key := string(n.Channel) + ":" + n.Recipient
	ok, err := v.limiter.Allow(ctx, key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("rate limit exceeded for recipient %q on channel %s", n.Recipient, n.Channel)
	}
	return nil
}
