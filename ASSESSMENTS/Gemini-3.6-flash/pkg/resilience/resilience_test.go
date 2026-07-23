package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecuteWithRetry_Success(t *testing.T) {
	attempts := 0
	err := ExecuteWithRetry(context.Background(), RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Factor:       2.0,
		Jitter:       false,
	}, func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestExecuteWithRetry_ExceedMaxAttempts(t *testing.T) {
	attempts := 0
	expectedErr := errors.New("persistent error")
	err := ExecuteWithRetry(context.Background(), RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		Factor:       2.0,
		Jitter:       false,
	}, func(ctx context.Context) error {
		attempts++
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestCircuitBreaker_TripsAndResets(t *testing.T) {
	cb := NewCircuitBreaker(2, 1, 50*time.Millisecond)

	failErr := errors.New("service down")

	// Call 1 fails
	_ = cb.Execute(func() error { return failErr })
	if cb.State() != StateClosed {
		t.Fatalf("expected CLOSED state after 1 failure, got %s", cb.State())
	}

	// Call 2 fails -> should trip circuit to OPEN
	_ = cb.Execute(func() error { return failErr })
	if cb.State() != StateOpen {
		t.Fatalf("expected OPEN state after 2 failures, got %s", cb.State())
	}

	// Call 3 while open -> should return ErrCircuitOpen immediately
	err := cb.Execute(func() error { return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Call after reset timeout -> enters HALF_OPEN and succeeds -> transitions to CLOSED
	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("expected success in half open state, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Fatalf("expected CLOSED state after success in HALF_OPEN, got %s", cb.State())
	}
}
