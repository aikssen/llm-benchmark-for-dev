package delivery

import (
	"testing"
	"time"
)

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)

	// Below threshold: still closed and allowing.
	cb.Failure()
	cb.Failure()
	if !cb.Allow() {
		t.Fatal("breaker should be closed before threshold")
	}
	cb.Failure() // third failure trips it
	if cb.Allow() {
		t.Fatal("breaker should be open after reaching threshold")
	}
}

func TestCircuitBreaker_HalfOpenProbeSucceeds(t *testing.T) {
	now := time.Unix(0, 0)
	cb := NewCircuitBreaker(1, time.Minute)
	cb.now = func() time.Time { return now }

	cb.Failure() // opens immediately (threshold 1)
	if cb.Allow() {
		t.Fatal("should be open")
	}

	// After cooldown, one probe is allowed (half-open).
	now = now.Add(time.Minute + time.Second)
	if !cb.Allow() {
		t.Fatal("probe should be allowed after cooldown")
	}
	// A second concurrent probe is blocked.
	if cb.Allow() {
		t.Fatal("only one half-open probe should be allowed")
	}
	// Probe succeeds -> breaker closes.
	cb.Success()
	if !cb.Allow() {
		t.Fatal("breaker should be closed after successful probe")
	}
}

func TestCircuitBreaker_HalfOpenProbeFailsReopens(t *testing.T) {
	now := time.Unix(0, 0)
	cb := NewCircuitBreaker(1, time.Minute)
	cb.now = func() time.Time { return now }

	cb.Failure()
	now = now.Add(time.Minute + time.Second)
	if !cb.Allow() {
		t.Fatal("probe should be allowed")
	}
	cb.Failure() // probe fails -> reopen
	if cb.Allow() {
		t.Fatal("breaker should reopen after failed probe")
	}
}

func TestCircuitBreaker_Disabled(t *testing.T) {
	cb := NewCircuitBreaker(0, time.Minute)
	for range 100 {
		cb.Failure()
	}
	if !cb.Allow() {
		t.Fatal("disabled breaker should always allow")
	}
}
