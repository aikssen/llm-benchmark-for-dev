// Package ratelimit provides the per-recipient throttling used to protect
// users from being spammed and providers from being overwhelmed.
package ratelimit

import (
	"sync"
	"time"
)

// Config describes a token bucket.
type Config struct {
	// Rate is the sustained number of allowed events per Interval.
	Rate int
	// Interval is the window over which Rate applies.
	Interval time.Duration
	// Burst is the maximum number of events allowed back to back. It
	// defaults to Rate when left at zero.
	Burst int
}

// TokenBucket is a keyed token-bucket limiter.
//
// A token bucket is preferred over a fixed window because it tolerates the
// bursty traffic this service is expected to receive without allowing twice
// the limit across a window boundary.
//
// Buckets are kept in memory, which means limits are per instance. That is
// documented as a deliberate trade-off; a shared limiter (Redis) is the
// production follow-up. The Limiter interface makes that swap local.
type TokenBucket struct {
	rate     float64 // tokens per second
	burst    float64
	now      func() time.Time
	mu       sync.Mutex
	buckets  map[string]*bucket
	lastGC   time.Time
	idleFor  time.Duration
	gcPeriod time.Duration
}

type bucket struct {
	tokens float64
	last   time.Time
}

// Option customises a TokenBucket.
type Option func(*TokenBucket)

// WithClock replaces the time source, so tests do not have to sleep.
func WithClock(now func() time.Time) Option {
	return func(t *TokenBucket) { t.now = now }
}

// New builds a limiter. A non-positive rate disables limiting entirely.
func New(cfg Config, opts ...Option) *TokenBucket {
	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = cfg.Rate
	}

	tb := &TokenBucket{
		rate:     float64(cfg.Rate) / interval.Seconds(),
		burst:    float64(burst),
		now:      time.Now,
		buckets:  make(map[string]*bucket),
		idleFor:  10 * interval,
		gcPeriod: interval,
	}
	for _, opt := range opts {
		opt(tb)
	}
	tb.lastGC = tb.now()
	return tb
}

// Allow consumes one token for key, reporting whether it was available.
func (t *TokenBucket) Allow(key string) bool {
	if t.rate <= 0 {
		return true // limiting disabled
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	t.collectLocked(now)

	b, ok := t.buckets[key]
	if !ok {
		b = &bucket{tokens: t.burst, last: now}
		t.buckets[key] = b
	} else {
		if elapsed := now.Sub(b.last); elapsed > 0 {
			b.tokens = min(t.burst, b.tokens+elapsed.Seconds()*t.rate)
		}
		b.last = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// collectLocked drops buckets that have been idle long enough to have fully
// refilled, so that a long-lived process does not accumulate one entry per
// recipient ever seen. Callers must hold the mutex.
func (t *TokenBucket) collectLocked(now time.Time) {
	if now.Sub(t.lastGC) < t.gcPeriod {
		return
	}
	t.lastGC = now
	for key, b := range t.buckets {
		if now.Sub(b.last) > t.idleFor {
			delete(t.buckets, key)
		}
	}
}

// Len reports the number of tracked keys. Intended for tests and metrics.
func (t *TokenBucket) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.buckets)
}
