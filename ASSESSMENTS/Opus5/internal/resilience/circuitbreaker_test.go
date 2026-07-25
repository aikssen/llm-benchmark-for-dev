package resilience_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/resilience"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newBreaker(cfg resilience.BreakerConfig) (*resilience.CircuitBreaker, *testClock) {
	clock := &testClock{now: time.Unix(0, 0)}
	return resilience.NewCircuitBreaker("test-provider", cfg, resilience.WithBreakerClock(clock.Now)), clock
}

// fail runs the breaker with an operation that always fails transiently.
func fail(b *resilience.CircuitBreaker) error {
	return b.Execute(context.Background(),
		func(context.Context) error { return errTransient },
		resilience.IsRetryable)
}

// succeed runs the breaker with an operation that always succeeds.
func succeed(b *resilience.CircuitBreaker) error {
	return b.Execute(context.Background(),
		func(context.Context) error { return nil },
		resilience.IsRetryable)
}

func TestBreakerStartsClosed(t *testing.T) {
	t.Parallel()

	b, _ := newBreaker(resilience.DefaultBreakerConfig())
	if got := b.State(); got != resilience.StateClosed {
		t.Errorf("State() = %q, want %q", got, resilience.StateClosed)
	}
}

func TestBreakerTripsAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()

	b, _ := newBreaker(resilience.BreakerConfig{FailureThreshold: 3, OpenDuration: time.Minute})

	for i := range 3 {
		if err := fail(b); !errors.Is(err, errTransient) {
			t.Fatalf("failure %d returned %v, want the underlying error", i+1, err)
		}
	}
	if got := b.State(); got != resilience.StateOpen {
		t.Fatalf("State() = %q after reaching the threshold, want %q", got, resilience.StateOpen)
	}

	// Once open, calls are refused without reaching the provider.
	called := false
	err := b.Execute(context.Background(), func(context.Context) error {
		called = true
		return nil
	}, resilience.IsRetryable)

	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Errorf("Execute() = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Error("the provider was called while the breaker was open")
	}
}

func TestBreakerResetsTheFailureCountOnSuccess(t *testing.T) {
	t.Parallel()

	b, _ := newBreaker(resilience.BreakerConfig{FailureThreshold: 3, OpenDuration: time.Minute})

	_ = fail(b)
	_ = fail(b)
	_ = succeed(b) // the streak is broken
	_ = fail(b)
	_ = fail(b)

	if got := b.State(); got != resilience.StateClosed {
		t.Errorf("State() = %q, want the breaker to stay closed: failures were not consecutive", got)
	}
}

// A provider rejecting one malformed message says nothing about its health
// and must not take the channel down for every other recipient.
func TestBreakerIgnoresPermanentFailures(t *testing.T) {
	t.Parallel()

	b, _ := newBreaker(resilience.BreakerConfig{FailureThreshold: 2, OpenDuration: time.Minute})

	for range 10 {
		err := b.Execute(context.Background(),
			func(context.Context) error { return errPermanent },
			resilience.IsRetryable)
		if !errors.Is(err, errPermanent) {
			t.Fatalf("Execute() = %v, want the permanent error", err)
		}
	}
	if got := b.State(); got != resilience.StateClosed {
		t.Errorf("State() = %q, want %q: message-level rejections are not provider failures",
			got, resilience.StateClosed)
	}
}

func TestBreakerProbesAfterTheOpenDurationAndCloses(t *testing.T) {
	t.Parallel()

	b, clock := newBreaker(resilience.BreakerConfig{
		FailureThreshold: 2,
		OpenDuration:     5 * time.Second,
		HalfOpenMaxCalls: 2,
		SuccessThreshold: 2,
	})

	_ = fail(b)
	_ = fail(b)
	if got := b.State(); got != resilience.StateOpen {
		t.Fatalf("State() = %q, want %q", got, resilience.StateOpen)
	}

	// Still open before the cooldown elapses.
	clock.Advance(4 * time.Second)
	if got := b.State(); got != resilience.StateOpen {
		t.Fatalf("State() = %q before the cooldown elapsed, want %q", got, resilience.StateOpen)
	}

	clock.Advance(2 * time.Second)
	if got := b.State(); got != resilience.StateHalfOpen {
		t.Fatalf("State() = %q after the cooldown, want %q", got, resilience.StateHalfOpen)
	}

	if err := succeed(b); err != nil {
		t.Fatalf("first probe = %v, want nil", err)
	}
	if got := b.State(); got != resilience.StateHalfOpen {
		t.Errorf("State() = %q after one probe, want to stay %q until the success threshold is met",
			got, resilience.StateHalfOpen)
	}
	if err := succeed(b); err != nil {
		t.Fatalf("second probe = %v, want nil", err)
	}
	if got := b.State(); got != resilience.StateClosed {
		t.Errorf("State() = %q after enough successful probes, want %q", got, resilience.StateClosed)
	}
}

func TestBreakerReopensImmediatelyWhenAProbeFails(t *testing.T) {
	t.Parallel()

	b, clock := newBreaker(resilience.BreakerConfig{
		FailureThreshold: 5,
		OpenDuration:     time.Second,
		HalfOpenMaxCalls: 1,
	})

	for range 5 {
		_ = fail(b)
	}
	clock.Advance(2 * time.Second)
	if got := b.State(); got != resilience.StateHalfOpen {
		t.Fatalf("State() = %q, want %q", got, resilience.StateHalfOpen)
	}

	// One failed probe is enough: no need to wait for another five failures.
	_ = fail(b)
	if got := b.State(); got != resilience.StateOpen {
		t.Errorf("State() = %q after a failed probe, want %q", got, resilience.StateOpen)
	}
}

func TestBreakerLimitsConcurrentProbes(t *testing.T) {
	t.Parallel()

	b, clock := newBreaker(resilience.BreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     time.Second,
		HalfOpenMaxCalls: 1,
		SuccessThreshold: 5,
	})

	_ = fail(b)
	clock.Advance(2 * time.Second)

	// Hold the single probe slot, then check that a second call is refused.
	release := make(chan struct{})
	probing := make(chan struct{})
	go func() {
		_ = b.Execute(context.Background(), func(context.Context) error {
			close(probing)
			<-release
			return nil
		}, resilience.IsRetryable)
	}()

	<-probing
	err := b.Execute(context.Background(), func(context.Context) error {
		t.Error("a second probe reached the provider while one was in flight")
		return nil
	}, resilience.IsRetryable)

	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Errorf("Execute() = %v, want ErrCircuitOpen while the probe slot is taken", err)
	}
	close(release)
}

func TestBreakerWithZeroThresholdIsDisabled(t *testing.T) {
	t.Parallel()

	b, _ := newBreaker(resilience.BreakerConfig{FailureThreshold: 0})
	for range 100 {
		if err := fail(b); !errors.Is(err, errTransient) {
			t.Fatalf("Execute() = %v, want the call to always reach the provider", err)
		}
	}
}

func TestBreakerIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	b, _ := newBreaker(resilience.BreakerConfig{FailureThreshold: 10, OpenDuration: time.Millisecond})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = fail(b)
				return
			}
			_ = succeed(b)
			_ = b.State()
		}(i)
	}
	wg.Wait()
}
