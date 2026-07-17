package validation

import (
	"context"
	"sync"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
)

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

type RateLimitValidator struct {
	limiter RateLimiter
}

func NewRateLimitValidator(l RateLimiter) *RateLimitValidator {
	return &RateLimitValidator{limiter: l}
}

func (RateLimitValidator) Name() string { return "rate_limit" }

func (v *RateLimitValidator) Validate(ctx context.Context, n *domain.Notification) error {
	ok, err := v.limiter.Allow(ctx, string(n.Channel)+":"+n.Recipient)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrRateLimited
	}
	return nil
}

type TokenBucket struct {
	capacity   int
	refillRate float64
	now        func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewTokenBucket(capacity int, refillPerSecond float64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		refillRate: refillPerSecond,
		now:        time.Now,
		buckets:    make(map[string]*bucket),
	}
}

func (t *TokenBucket) Allow(_ context.Context, key string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	b, ok := t.buckets[key]
	if !ok {
		t.buckets[key] = &bucket{tokens: float64(t.capacity) - 1, last: now}
		return true, nil
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * t.refillRate
	if b.tokens > float64(t.capacity) {
		b.tokens = float64(t.capacity)
	}
	b.last = now
	if b.tokens < 1 {
		return false, nil
	}
	b.tokens--
	return true, nil
}
