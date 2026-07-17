package validator

import (
	"context"
	"sync"
	"time"

	"github.com/notifications-service/internal/domain"
)

type RateLimitValidator struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewRateLimitValidator(limit int, window time.Duration) *RateLimitValidator {
	return &RateLimitValidator{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (v *RateLimitValidator) Name() string {
	return "rate_limit"
}

func (v *RateLimitValidator) Validate(_ context.Context, recipient string, _ *domain.NotificationRequest) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-v.window)

	requests := v.requests[recipient]
	var valid []time.Time
	for _, t := range requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= v.limit {
		return domain.ErrRateLimitExceeded
	}

	v.requests[recipient] = append(valid, now)
	return nil
}
