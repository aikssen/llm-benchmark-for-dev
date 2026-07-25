package validation

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/preferences"
	"github.com/evercifuentes/notifier/internal/provider"
)

// Rejection codes produced by the built-in validators.
const (
	CodeInvalidRecipient = "invalid_recipient"
	CodeUnknownChannel   = "unsupported_channel"
	CodeOptedOut         = "recipient_opted_out"
	CodeBlocked          = "recipient_blocked"
	CodeRateLimited      = "rate_limited"
	CodeInvalidContent   = "invalid_content"
)

// --- recipient -------------------------------------------------------------

// RecipientValidator checks that the destination is well formed for the
// channel. The rule itself lives in the provider, so a new channel brings its
// own address format with it.
type RecipientValidator struct {
	registry *provider.Registry
}

// NewRecipientValidator builds the validator.
func NewRecipientValidator(registry *provider.Registry) *RecipientValidator {
	return &RecipientValidator{registry: registry}
}

func (v *RecipientValidator) Name() string { return "recipient" }

func (v *RecipientValidator) Validate(_ context.Context, s Subject) error {
	if strings.TrimSpace(s.Recipient.Address) == "" {
		return Reject(CodeInvalidRecipient, "recipient address is required")
	}
	p, err := v.registry.Get(s.Channel)
	if err != nil {
		return Reject(CodeUnknownChannel, "channel %q is not supported", s.Channel)
	}
	if err := p.ValidateAddress(s.Recipient.Address); err != nil {
		return Reject(CodeInvalidRecipient, "recipient address %q is invalid: %s", s.Recipient.Address, err)
	}
	return nil
}

// --- preferences -----------------------------------------------------------

// PreferenceValidator refuses deliveries the recipient has opted out of.
type PreferenceValidator struct {
	store preferences.Store
}

// NewPreferenceValidator builds the validator.
func NewPreferenceValidator(store preferences.Store) *PreferenceValidator {
	return &PreferenceValidator{store: store}
}

func (v *PreferenceValidator) Name() string { return "preferences" }

func (v *PreferenceValidator) Validate(ctx context.Context, s Subject) error {
	prefs, err := v.store.Get(ctx, s.Recipient.Key())
	if err != nil {
		// The preference service is unreachable. This is *not* a rejection:
		// returning the raw error lets the caller retry rather than
		// permanently discarding a legitimate notification.
		return fmt.Errorf("look up preferences for %q: %w", s.Recipient.Key(), err)
	}
	if prefs.Blocked {
		return Reject(CodeBlocked, "recipient is blocked from receiving notifications")
	}
	if !prefs.Allows(s.Channel) {
		return Reject(CodeOptedOut, "recipient has opted out of the %q channel", s.Channel)
	}
	return nil
}

// --- rate limit ------------------------------------------------------------

// Limiter is the throttling port. Allow must be safe for concurrent use.
type Limiter interface {
	Allow(key string) bool
}

// RateLimitValidator caps how often a single recipient can be contacted on a
// channel.
type RateLimitValidator struct {
	limiter Limiter
}

// NewRateLimitValidator builds the validator.
func NewRateLimitValidator(limiter Limiter) *RateLimitValidator {
	return &RateLimitValidator{limiter: limiter}
}

func (v *RateLimitValidator) Name() string { return "rate_limit" }

func (v *RateLimitValidator) Validate(_ context.Context, s Subject) error {
	// Limits are scoped per recipient *and* channel: an email quota should
	// not consume the SMS quota.
	key := s.Channel.String() + ":" + s.Recipient.Key()
	if !v.limiter.Allow(key) {
		return Reject(CodeRateLimited, "rate limit exceeded for recipient on the %q channel", s.Channel)
	}
	return nil
}

// --- content ---------------------------------------------------------------

// ContentLimits are the per-channel constraints on message content.
type ContentLimits struct {
	MaxBodyLength    int
	MaxSubjectLength int
	// SubjectRequired marks channels where an empty subject is meaningless.
	SubjectRequired bool
}

// ContentValidator checks the message against the limits of its channel.
type ContentValidator struct {
	limits   map[domain.Channel]ContentLimits
	fallback ContentLimits
}

// NewContentValidator builds the validator. Channels missing from limits fall
// back to the given default, so an unfamiliar channel is still bounded.
func NewContentValidator(limits map[domain.Channel]ContentLimits, fallback ContentLimits) *ContentValidator {
	return &ContentValidator{limits: limits, fallback: fallback}
}

func (v *ContentValidator) Name() string { return "content" }

func (v *ContentValidator) Validate(_ context.Context, s Subject) error {
	limits, ok := v.limits[s.Channel]
	if !ok {
		limits = v.fallback
	}

	if strings.TrimSpace(s.Message.Body) == "" {
		return Reject(CodeInvalidContent, "message body is required")
	}
	// Count runes, not bytes: a 160-character SMS limit is about characters.
	if n := utf8.RuneCountInString(s.Message.Body); limits.MaxBodyLength > 0 && n > limits.MaxBodyLength {
		return Reject(CodeInvalidContent, "message body is %d characters, the limit for %q is %d",
			n, s.Channel, limits.MaxBodyLength)
	}
	if limits.SubjectRequired && strings.TrimSpace(s.Message.Subject) == "" {
		return Reject(CodeInvalidContent, "a subject is required for the %q channel", s.Channel)
	}
	if n := utf8.RuneCountInString(s.Message.Subject); limits.MaxSubjectLength > 0 && n > limits.MaxSubjectLength {
		return Reject(CodeInvalidContent, "subject is %d characters, the limit for %q is %d",
			n, s.Channel, limits.MaxSubjectLength)
	}
	return nil
}

// DefaultContentLimits returns limits derived from what the real transports
// accept: SMS is billed per segment, push payloads are capped by the mobile
// platforms, email bodies are effectively unbounded but need a ceiling.
func DefaultContentLimits() map[domain.Channel]ContentLimits {
	return map[domain.Channel]ContentLimits{
		domain.ChannelEmail: {MaxBodyLength: 100_000, MaxSubjectLength: 255, SubjectRequired: true},
		domain.ChannelSMS:   {MaxBodyLength: 1_600},
		domain.ChannelPush:  {MaxBodyLength: 2_000, MaxSubjectLength: 100, SubjectRequired: true},
	}
}
