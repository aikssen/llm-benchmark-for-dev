// Package resilience holds the policies that turn unreliable external calls
// into a best-effort delivery guarantee: bounded retries with exponential
// backoff, and a circuit breaker that stops hammering a provider that is
// already down.
package resilience

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

// Retryable is implemented by errors that know whether trying again could
// help. Errors that do not implement it are treated as permanent: silently
// retrying an unclassified error is how a service ends up sending duplicates
// or spinning on a bug.
type Retryable interface {
	Retryable() bool
}

// IsRetryable reports whether err is worth retrying.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var r Retryable
	if errors.As(err, &r) {
		return r.Retryable()
	}
	return false
}

// RetryPolicy configures bounded exponential backoff with jitter.
type RetryPolicy struct {
	// MaxAttempts is the total number of tries, including the first one.
	// Values below 1 are treated as 1.
	MaxAttempts int
	// InitialBackoff is the delay after the first failure.
	InitialBackoff time.Duration
	// MaxBackoff caps the delay.
	MaxBackoff time.Duration
	// Multiplier grows the delay between attempts. Values below 1 are
	// treated as 1 (constant backoff).
	Multiplier float64
	// Jitter is the fraction of the delay randomised, in [0,1]. Jitter
	// matters here: without it, a burst of deliveries that fail together
	// retries together and re-creates the spike that took the provider down.
	Jitter float64
}

// DefaultRetryPolicy is a conservative starting point.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		Multiplier:     2,
		Jitter:         0.2,
	}
}

// Backoff returns the delay to wait after the given attempt (1-based).
func (p RetryPolicy) Backoff(attempt int, random func() float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	multiplier := p.Multiplier
	if multiplier < 1 {
		multiplier = 1
	}

	delay := float64(p.InitialBackoff) * math.Pow(multiplier, float64(attempt-1))
	if p.MaxBackoff > 0 && delay > float64(p.MaxBackoff) {
		delay = float64(p.MaxBackoff)
	}
	if p.Jitter > 0 && random != nil {
		// Randomise symmetrically around the nominal delay.
		jitter := delay * p.Jitter * (2*random() - 1)
		delay += jitter
	}
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// Retrier executes an operation according to a RetryPolicy.
type Retrier struct {
	policy RetryPolicy
	random func() float64
	sleep  func(ctx context.Context, d time.Duration) error
}

// RetrierOption customises a Retrier, mainly for deterministic tests.
type RetrierOption func(*Retrier)

// WithRandom replaces the jitter source.
func WithRandom(f func() float64) RetrierOption {
	return func(r *Retrier) { r.random = f }
}

// WithSleep replaces the backoff wait.
func WithSleep(f func(ctx context.Context, d time.Duration) error) RetrierOption {
	return func(r *Retrier) { r.sleep = f }
}

// NewRetrier builds a Retrier.
func NewRetrier(policy RetryPolicy, opts ...RetrierOption) *Retrier {
	r := &Retrier{
		policy: policy,
		random: rand.Float64, // math/rand/v2 top-level funcs are goroutine-safe
		sleep:  sleepCtx,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Do runs op until it succeeds, returns a non-retryable error, exhausts the
// attempt budget, or the context is done.
//
// op receives the 1-based attempt number. The returned attempt count is the
// number of times op actually ran, which is what the caller records on the
// delivery.
func (r *Retrier) Do(ctx context.Context, op func(ctx context.Context, attempt int) error) (attempts int, err error) {
	maxAttempts := max(r.policy.MaxAttempts, 1)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if err == nil {
				err = ctxErr
			}
			return attempts, err
		}

		attempts = attempt
		err = op(ctx, attempt)
		switch {
		case err == nil:
			return attempts, nil
		case !IsRetryable(err):
			return attempts, err
		case attempt == maxAttempts:
			return attempts, err
		}

		if waitErr := r.sleep(ctx, r.policy.Backoff(attempt, r.random)); waitErr != nil {
			// The caller gave up while we were backing off. Report the
			// provider failure, which is the more useful diagnosis.
			return attempts, err
		}
	}
	return attempts, err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
