package provider

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned (wrapped) when the circuit breaker is open
// and calls are being short-circuited to protect an unavailable provider.
var ErrCircuitOpen = errors.New("circuit breaker is open")

type breakerState int

const (
	stateClosed breakerState = iota
	stateOpen
	stateHalfOpen
)

// CircuitBreaker stops hammering a provider that appears to be down.
//
//	closed  -> all calls allowed; consecutiveFailures >= threshold opens it
//	open    -> calls rejected immediately for resetTimeout
//	half-open -> a single probe call is allowed; success closes, failure re-opens
//
// It is goroutine-safe.
type CircuitBreaker struct {
	mu        sync.Mutex
	state     breakerState
	failures  int
	threshold int
	resetWait time.Duration
	openedAt  time.Time
	now       func() time.Time // injectable for tests
}

// NewCircuitBreaker opens after threshold consecutive failures and stays
// open for resetWait before allowing a probe.
func NewCircuitBreaker(threshold int, resetWait time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		resetWait: resetWait,
		now:       time.Now,
	}
}

// Allow reports whether a call may proceed.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case stateOpen:
		if cb.now().Sub(cb.openedAt) >= cb.resetWait {
			cb.state = stateHalfOpen
			return nil // single probe allowed
		}
		return ErrCircuitOpen
	default:
		return nil
	}
}

// OnSuccess records a successful call.
func (cb *CircuitBreaker) OnSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = stateClosed
}

// OnFailure records a failed call, opening the breaker when the
// threshold is reached.
func (cb *CircuitBreaker) OnFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == stateHalfOpen || cb.failures >= cb.threshold {
		cb.state = stateOpen
		cb.openedAt = cb.now()
	}
}
