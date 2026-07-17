package validator

import (
	"context"
	"sync"
	"time"

	"github.com/notification-service/internal/domain"
	apperrors "github.com/notification-service/pkg/errors"
)

// RateLimiterValidator enforces rate limits per recipient.
type RateLimiterValidator struct {
	mu       sync.Mutex
	limiters map[string]*tokenBucket
	rate     int
	interval time.Duration
}

type tokenBucket struct {
	tokens    int
	lastReset time.Time
}

// NewRateLimiterValidator creates a new rate limiter validator.
// rate: maximum number of notifications per interval.
// interval: the time window for rate limiting.
func NewRateLimiterValidator(rate int, interval time.Duration) *RateLimiterValidator {
	return &RateLimiterValidator{
		limiters: make(map[string]*tokenBucket),
		rate:     rate,
		interval: interval,
	}
}

func (v *RateLimiterValidator) Name() string {
	return "rate_limiter"
}

func (v *RateLimiterValidator) Validate(ctx context.Context, req *domain.CreateNotificationRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	for _, recipient := range req.Recipients {
		if !v.allow(recipient.Value) {
			return &apperrors.RateLimitError{
				Message: "rate limit exceeded for recipient: " + recipient.Value,
			}
		}
	}

	return nil
}

func (v *RateLimiterValidator) allow(key string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	bucket, exists := v.limiters[key]
	if !exists {
		bucket = &tokenBucket{
			tokens:    v.rate,
			lastReset: time.Now(),
		}
		v.limiters[key] = bucket
	}

	if time.Since(bucket.lastReset) >= v.interval {
		bucket.tokens = v.rate
		bucket.lastReset = time.Now()
	}

	if bucket.tokens <= 0 {
		return false
	}

	bucket.tokens--
	return true
}
