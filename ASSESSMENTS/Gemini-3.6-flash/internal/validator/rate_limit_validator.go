package validator

import (
	"context"
	"sync"
	"time"

	"notification-service/internal/domain"
)

type RateLimitValidator struct {
	mu          sync.Mutex
	maxRequests int
	window      time.Duration
	history     map[string][]time.Time
}

func NewRateLimitValidator(maxRequests int, window time.Duration) *RateLimitValidator {
	return &RateLimitValidator{
		maxRequests: maxRequests,
		window:      window,
		history:     make(map[string][]time.Time),
	}
}

func (rv *RateLimitValidator) Name() string {
	return "RateLimitValidator"
}

func (rv *RateLimitValidator) Validate(ctx context.Context, item *domain.SingleRecipientItem) error {
	rv.mu.Lock()
	defer rv.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rv.window)

	// Clean up old entries
	times := rv.history[item.Recipient]
	var validTimes []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			validTimes = append(validTimes, t)
		}
	}

	if len(validTimes) >= rv.maxRequests {
		return domain.ErrRateLimitExceeded
	}

	rv.history[item.Recipient] = append(validTimes, now)
	return nil
}
