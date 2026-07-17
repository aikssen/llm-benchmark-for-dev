package provider

import (
	"context"
	"math/rand"
	"time"

	"notifications/internal/domain"
)

// ResilientProvider wraps a Provider with:
//
//   - a per-attempt timeout (protects against slow providers),
//   - retries with exponential backoff and jitter (absorbs intermittent
//     errors and temporary failures),
//   - a circuit breaker (stops wasting resources on unavailable providers).
//
// The number of attempts performed is recorded on the Notification.
type ResilientProvider struct {
	inner       Provider
	breaker     *CircuitBreaker
	timeout     time.Duration // per attempt
	maxAttempts int
	baseDelay   time.Duration // backoff base; doubles every attempt
	maxDelay    time.Duration // backoff cap

	// sleep is injectable so tests don't wait on real backoff.
	sleep func(ctx context.Context, d time.Duration) error
	rng   *rand.Rand
}

// ResilientConfig tunes a ResilientProvider.
type ResilientConfig struct {
	AttemptTimeout   time.Duration
	MaxAttempts      int
	BaseDelay        time.Duration
	MaxDelay         time.Duration
	BreakerThreshold int
	BreakerResetWait time.Duration
}

// Wrap makes p resilient according to cfg.
func Wrap(p Provider, cfg ResilientConfig) *ResilientProvider {
	return &ResilientProvider{
		inner:       p,
		breaker:     NewCircuitBreaker(cfg.BreakerThreshold, cfg.BreakerResetWait),
		timeout:     cfg.AttemptTimeout,
		maxAttempts: cfg.MaxAttempts,
		baseDelay:   cfg.BaseDelay,
		maxDelay:    cfg.MaxDelay,
		sleep:       contextSleep,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (r *ResilientProvider) Channel() domain.Channel { return r.inner.Channel() }

// Send attempts delivery until success, a permanent error, or exhaustion
// of attempts. It increments n.Attempts for every real attempt made.
func (r *ResilientProvider) Send(ctx context.Context, n *domain.Notification) error {
	var lastErr error

	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		if err := r.breaker.Allow(); err != nil {
			// Fail fast while the provider is down; treated as temporary
			// so the retry loop waits for the half-open probe window.
			lastErr = Temporaryf("%v", err)
		} else {
			n.Attempts++
			err := r.try(ctx, n)
			if err == nil {
				r.breaker.OnSuccess()
				return nil
			}
			if IsTemporary(err) {
				r.breaker.OnFailure()
			} else {
				// Permanent failure: do not retry, do not trip the
				// breaker (the provider is healthy, the request is bad).
				return err
			}
			lastErr = err
		}

		if attempt < r.maxAttempts {
			if err := r.sleep(ctx, r.backoff(attempt)); err != nil {
				return err // context cancelled (e.g. shutdown)
			}
		}
	}
	return Temporaryf("delivery failed after %d attempt(s): %v", r.maxAttempts, lastErr)
}

// try performs a single attempt with a timeout.
func (r *ResilientProvider) try(ctx context.Context, n *domain.Notification) error {
	attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.inner.Send(attemptCtx, n); err != nil {
		if attemptCtx.Err() != nil && ctx.Err() == nil {
			return Temporaryf("provider timed out after %s", r.timeout)
		}
		return err
	}
	return nil
}

// backoff computes base*2^(attempt-1) capped at maxDelay, plus up to 25%
// jitter to avoid thundering-herd retries.
func (r *ResilientProvider) backoff(attempt int) time.Duration {
	d := r.baseDelay << (attempt - 1)
	if d <= 0 || d > r.maxDelay {
		d = r.maxDelay
	}
	jitter := time.Duration(r.rng.Int63n(int64(d)/4 + 1))
	return d + jitter
}

func contextSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
