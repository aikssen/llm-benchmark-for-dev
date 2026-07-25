package ratelimit_test

import (
	"sync"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/ratelimit"
)

// fakeClock lets the tests advance time without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestTokenBucketAllowsTheBurstThenRefuses(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter := ratelimit.New(
		ratelimit.Config{Rate: 60, Interval: time.Minute, Burst: 3},
		ratelimit.WithClock(clock.Now),
	)

	for i := range 3 {
		if !limiter.Allow("u1") {
			t.Fatalf("call %d was refused, the burst of 3 should be allowed", i+1)
		}
	}
	if limiter.Allow("u1") {
		t.Fatal("the fourth call was allowed, the burst is exhausted")
	}
}

func TestTokenBucketRefillsOverTime(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(0, 0)}
	// 60 per minute = one token per second.
	limiter := ratelimit.New(
		ratelimit.Config{Rate: 60, Interval: time.Minute, Burst: 1},
		ratelimit.WithClock(clock.Now),
	)

	if !limiter.Allow("u1") {
		t.Fatal("the first call should be allowed")
	}
	if limiter.Allow("u1") {
		t.Fatal("the second immediate call should be refused")
	}

	clock.Advance(time.Second)
	if !limiter.Allow("u1") {
		t.Fatal("a token should have been refilled after one second")
	}
}

func TestTokenBucketRefillIsCappedAtTheBurst(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter := ratelimit.New(
		ratelimit.Config{Rate: 60, Interval: time.Minute, Burst: 2},
		ratelimit.WithClock(clock.Now),
	)

	// Idle for an hour: the bucket must not accumulate 3600 tokens.
	clock.Advance(time.Hour)
	allowed := 0
	for range 10 {
		if limiter.Allow("u1") {
			allowed++
		}
	}
	if allowed != 2 {
		t.Errorf("allowed %d calls after a long idle period, want the burst of 2", allowed)
	}
}

func TestTokenBucketKeysAreIndependent(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter := ratelimit.New(
		ratelimit.Config{Rate: 1, Interval: time.Minute, Burst: 1},
		ratelimit.WithClock(clock.Now),
	)

	if !limiter.Allow("u1") || !limiter.Allow("u2") {
		t.Fatal("different keys must not share a budget")
	}
	if limiter.Allow("u1") {
		t.Error("u1's budget should be exhausted")
	}
}

func TestTokenBucketWithZeroRateIsDisabled(t *testing.T) {
	t.Parallel()

	limiter := ratelimit.New(ratelimit.Config{Rate: 0, Interval: time.Minute})
	for range 100 {
		if !limiter.Allow("u1") {
			t.Fatal("a zero rate must disable limiting rather than block everything")
		}
	}
}

// Idle buckets must be reclaimed, otherwise a long-running process
// accumulates one entry per recipient it has ever seen.
func TestTokenBucketReclaimsIdleBuckets(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter := ratelimit.New(
		ratelimit.Config{Rate: 60, Interval: time.Minute, Burst: 5},
		ratelimit.WithClock(clock.Now),
	)

	for _, key := range []string{"u1", "u2", "u3"} {
		limiter.Allow(key)
	}
	if got := limiter.Len(); got != 3 {
		t.Fatalf("tracked %d keys, want 3", got)
	}

	// Well past the idle threshold (10 intervals), then touch a new key to
	// trigger collection.
	clock.Advance(2 * time.Hour)
	limiter.Allow("u4")

	if got := limiter.Len(); got != 1 {
		t.Errorf("tracked %d keys after collection, want only the fresh one", got)
	}
}

func TestTokenBucketIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 32
		perRoutine = 50
		burst      = 100
	)
	// No refill during the test: the clock never moves, so the total number
	// of allowed calls must be exactly the burst.
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter := ratelimit.New(
		ratelimit.Config{Rate: 60, Interval: time.Minute, Burst: burst},
		ratelimit.WithClock(clock.Now),
	)

	var (
		mu      sync.Mutex
		allowed int
		wg      sync.WaitGroup
	)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for range perRoutine {
				if limiter.Allow("shared") {
					local++
				}
			}
			mu.Lock()
			allowed += local
			mu.Unlock()
		}()
	}
	wg.Wait()

	if allowed != burst {
		t.Errorf("allowed %d calls, want exactly the burst of %d", allowed, burst)
	}
}
