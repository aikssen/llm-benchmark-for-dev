package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/resilience"
)

// classifiedError implements the Retryable interface the policies consult.
type classifiedError struct {
	msg   string
	retry bool
}

func (e *classifiedError) Error() string   { return e.msg }
func (e *classifiedError) Retryable() bool { return e.retry }

var (
	errTransient = &classifiedError{msg: "temporarily unavailable", retry: true}
	errPermanent = &classifiedError{msg: "rejected", retry: false}
)

// noSleep makes backoff instantaneous and records what would have been waited.
func noSleep(waits *[]time.Duration) resilience.RetrierOption {
	return resilience.WithSleep(func(_ context.Context, d time.Duration) error {
		*waits = append(*waits, d)
		return nil
	})
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"transient", errTransient, true},
		{"permanent", errPermanent, false},
		{"wrapped transient", errors.Join(errors.New("context"), errTransient), true},
		// An unclassified error is treated as permanent: retrying an error we
		// do not understand risks duplicate sends.
		{"unclassified", errors.New("who knows"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resilience.IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRetrierStopsAtTheFirstSuccess(t *testing.T) {
	t.Parallel()

	var waits []time.Duration
	r := resilience.NewRetrier(resilience.RetryPolicy{MaxAttempts: 5, InitialBackoff: time.Second}, noSleep(&waits))

	calls := 0
	attempts, err := r.Do(context.Background(), func(context.Context, int) error {
		calls++
		if calls < 3 {
			return errTransient
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if attempts != 3 || calls != 3 {
		t.Errorf("attempts = %d, calls = %d, want 3 and 3", attempts, calls)
	}
	if len(waits) != 2 {
		t.Errorf("backed off %d times, want 2 (one after each failure)", len(waits))
	}
}

func TestRetrierDoesNotRetryPermanentFailures(t *testing.T) {
	t.Parallel()

	var waits []time.Duration
	r := resilience.NewRetrier(resilience.RetryPolicy{MaxAttempts: 5, InitialBackoff: time.Second}, noSleep(&waits))

	calls := 0
	attempts, err := r.Do(context.Background(), func(context.Context, int) error {
		calls++
		return errPermanent
	})

	if !errors.Is(err, errPermanent) {
		t.Fatalf("Do() = %v, want the permanent error", err)
	}
	if calls != 1 || attempts != 1 {
		t.Errorf("made %d calls (reported %d attempts), want exactly 1", calls, attempts)
	}
	if len(waits) != 0 {
		t.Error("backed off after a permanent failure")
	}
}

func TestRetrierExhaustsTheAttemptBudget(t *testing.T) {
	t.Parallel()

	var waits []time.Duration
	r := resilience.NewRetrier(resilience.RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Second}, noSleep(&waits))

	calls := 0
	attempts, err := r.Do(context.Background(), func(context.Context, int) error {
		calls++
		return errTransient
	})

	if !errors.Is(err, errTransient) {
		t.Fatalf("Do() = %v, want the transient error", err)
	}
	if calls != 3 || attempts != 3 {
		t.Errorf("made %d calls (reported %d attempts), want 3", calls, attempts)
	}
	// No backoff after the final attempt: there is nothing left to wait for.
	if len(waits) != 2 {
		t.Errorf("backed off %d times, want 2", len(waits))
	}
}

func TestRetrierPassesTheAttemptNumber(t *testing.T) {
	t.Parallel()

	var waits []time.Duration
	r := resilience.NewRetrier(resilience.RetryPolicy{MaxAttempts: 3}, noSleep(&waits))

	var seen []int
	_, _ = r.Do(context.Background(), func(_ context.Context, attempt int) error {
		seen = append(seen, attempt)
		return errTransient
	})

	want := []int{1, 2, 3}
	if len(seen) != len(want) {
		t.Fatalf("attempt numbers = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("attempt numbers = %v, want %v", seen, want)
		}
	}
}

func TestRetrierStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	r := resilience.NewRetrier(
		resilience.RetryPolicy{MaxAttempts: 5},
		resilience.WithSleep(func(context.Context, time.Duration) error {
			cancel() // the caller gives up while we are backing off
			return context.Canceled
		}),
	)

	calls := 0
	attempts, err := r.Do(ctx, func(context.Context, int) error {
		calls++
		return errTransient
	})

	if calls != 1 || attempts != 1 {
		t.Errorf("made %d calls, want 1 before the cancellation was noticed", calls)
	}
	// The provider failure is the more useful diagnosis, so it is preserved.
	if !errors.Is(err, errTransient) {
		t.Errorf("Do() = %v, want the underlying failure to be reported", err)
	}
}

func TestBackoffGrowsExponentiallyAndIsCapped(t *testing.T) {
	t.Parallel()

	policy := resilience.RetryPolicy{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     time.Second,
		Multiplier:     2,
	}
	// Jitter disabled so the progression is exact.
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{3, 400 * time.Millisecond},
		{4, 800 * time.Millisecond},
		{5, time.Second}, // capped
		{9, time.Second}, // still capped
	}
	for _, tt := range tests {
		if got := policy.Backoff(tt.attempt, nil); got != tt.want {
			t.Errorf("Backoff(%d) = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}

// Without jitter, deliveries that fail together retry together and recreate
// the spike that caused the failure.
func TestBackoffAppliesJitterAroundTheNominalDelay(t *testing.T) {
	t.Parallel()

	policy := resilience.RetryPolicy{
		InitialBackoff: time.Second,
		MaxBackoff:     time.Minute,
		Multiplier:     2,
		Jitter:         0.5,
	}

	if got, want := policy.Backoff(1, func() float64 { return 0 }), 500*time.Millisecond; got != want {
		t.Errorf("Backoff with minimum jitter = %s, want %s", got, want)
	}
	if got, want := policy.Backoff(1, func() float64 { return 1 }), 1500*time.Millisecond; got != want {
		t.Errorf("Backoff with maximum jitter = %s, want %s", got, want)
	}
	if got, want := policy.Backoff(1, func() float64 { return 0.5 }), time.Second; got != want {
		t.Errorf("Backoff with neutral jitter = %s, want %s", got, want)
	}
}

func TestBackoffIsNeverNegative(t *testing.T) {
	t.Parallel()

	policy := resilience.RetryPolicy{InitialBackoff: time.Millisecond, Jitter: 1, Multiplier: 2}
	if got := policy.Backoff(1, func() float64 { return 0 }); got < 0 {
		t.Errorf("Backoff() = %s, must never be negative", got)
	}
}
