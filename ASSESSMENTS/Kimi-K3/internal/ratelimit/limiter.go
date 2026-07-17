// Package ratelimit provides a small in-memory token-bucket limiter used
// to cap how many notifications a single recipient may receive over a
// time window.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter implements per-key token buckets. It is goroutine-safe.
type Limiter struct {
	mu      sync.Mutex
	limit   int           // tokens granted per window
	window  time.Duration // refill window
	buckets map[string]*bucket
	now     func() time.Time // injectable for tests

	// sweepEvery bounds how often expired buckets are garbage collected
	// so the map does not grow unbounded over time.
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New creates a Limiter that allows up to limit events per window per key.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow reports whether an event for key is allowed right now, consuming
// one token if so.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.maybeSweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.limit), last: now}
		l.buckets[key] = b
	}

	// Refill proportionally to elapsed time.
	elapsed := now.Sub(b.last)
	b.tokens += elapsed.Seconds() * (float64(l.limit) / l.window.Seconds())
	if b.tokens > float64(l.limit) {
		b.tokens = float64(l.limit)
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// maybeSweep removes buckets that have been idle longer than the window.
// Runs at most once per window to amortize the cost.
func (l *Limiter) maybeSweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.window {
			delete(l.buckets, k)
		}
	}
}
