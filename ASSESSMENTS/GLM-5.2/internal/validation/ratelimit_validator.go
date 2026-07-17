package validation

import (
	"context"
	"sync"
	"time"

	"github.com/example/notification-service/internal/domain"
)

// RateLimiter decides whether a recipient may receive a notification on a
// channel at this moment.
type RateLimiter interface {
	Allow(ctx context.Context, recipientID string, channel domain.Channel) (bool, error)
}

type rateLimitValidator struct {
	limiter RateLimiter
}

// NewRateLimitValidator returns a validator that enforces per-recipient,
// per-channel rate limits.
func NewRateLimitValidator(limiter RateLimiter) Validator {
	return &rateLimitValidator{limiter: limiter}
}

func (rateLimitValidator) Name() string { return "rate_limit" }

func (r rateLimitValidator) Validate(ctx context.Context, in Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if in.Recipient.ID == "" {
		return domain.NewValidationError("missing_recipient_id", "recipient.id", "recipient id is required for rate limiting")
	}
	allowed, err := r.limiter.Allow(ctx, in.Recipient.ID, in.Notification.Channel)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.NewValidationError("rate_limited", "rate_limit", "recipient exceeded the rate limit for this channel")
	}
	return nil
}

// SlidingWindowLimiter is an in-memory, per-key fixed-window rate limiter. It
// is safe for concurrent use. The window duration and max count are
// configurable per limiter.
type SlidingWindowLimiter struct {
	window time.Duration
	max    int
	now    func() time.Time
	mu     sync.Mutex
	hits   map[string][]time.Time
}

// NewSlidingWindowLimiter builds a limiter that allows at most max events per
// key within window.
func NewSlidingWindowLimiter(window time.Duration, max int) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		window: window,
		max:    max,
		now:    time.Now,
		hits:   make(map[string][]time.Time),
	}
}

// Allow reports whether the key may proceed. It prunes expired timestamps on
// every call, keeping memory bounded.
func (l *SlidingWindowLimiter) Allow(_ context.Context, recipientID string, channel domain.Channel) (bool, error) {
	key := recipientID + "|" + string(channel)
	cutoff := l.now().Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	hits := l.hits[key]
	// Drop expired entries.
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false, nil
	}
	l.hits[key] = append(kept, l.now())
	return true, nil
}
