package delivery

import (
	"math"
	"math/rand/v2"
	"time"
)

// RetryPolicy configures exponential backoff with full jitter. Jitter spreads
// retries from many concurrent workers so they do not synchronise into bursts
// that repeatedly hammer a recovering provider ("thundering herd").
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first; <1 means 1
	BaseDelay   time.Duration // delay before the first retry
	MaxDelay    time.Duration // cap on any single backoff delay
}

// DefaultRetryPolicy is a sensible baseline: up to 4 attempts with backoff
// growing from 100ms, capped at 5s.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}
}

// backoff returns the delay to wait before the given retry (retry 1 is the first
// retry, i.e. the second attempt). It uses exponential growth with full jitter:
// a random duration in [0, exp) where exp = min(BaseDelay * 2^(retry-1), MaxDelay).
func (p RetryPolicy) backoff(retry int) time.Duration {
	if retry < 1 {
		retry = 1
	}
	exp := float64(p.BaseDelay) * math.Pow(2, float64(retry-1))
	capped := math.Min(exp, float64(p.MaxDelay))
	if capped <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(capped)))
}

// attempts returns the effective number of attempts (never below 1).
func (p RetryPolicy) attempts() int {
	if p.MaxAttempts < 1 {
		return 1
	}
	return p.MaxAttempts
}
