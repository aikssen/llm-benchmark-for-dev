package delivery

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the breaker is open and rejecting calls.
var ErrCircuitOpen = errors.New("circuit breaker open")

// cbState is the breaker's current mode.
type cbState int

const (
	stateClosed   cbState = iota // normal operation, calls flow through
	stateOpen                    // failing fast, calls rejected until cooldown
	stateHalfOpen                // probing: a limited number of trial calls allowed
)

// CircuitBreaker protects an unreliable dependency from being hammered while it
// is down. After a run of failures it "opens" and rejects calls immediately for
// a cooldown period, then allows a single probe ("half-open") to decide whether
// to close again. This turns a slow/failing provider into fast local failures,
// preventing worker threads from piling up on a dead dependency.
//
// One breaker is kept per provider so that a single misbehaving channel does not
// affect the others.
type CircuitBreaker struct {
	maxFailures int           // consecutive failures before opening
	cooldown    time.Duration // how long to stay open before probing
	now         func() time.Time

	mu           sync.Mutex
	state        cbState
	failures     int
	openedAt     time.Time
	halfOpenBusy bool // a probe is in flight; block others until it resolves
}

// NewCircuitBreaker returns a breaker that opens after maxFailures consecutive
// failures and probes again after cooldown. A non-positive maxFailures disables
// the breaker (calls always allowed).
func NewCircuitBreaker(maxFailures int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		cooldown:    cooldown,
		now:         time.Now,
		state:       stateClosed,
	}
}

// Allow reports whether a call may proceed right now, transitioning state as
// needed. When it returns true in half-open mode it reserves the single probe
// slot; the caller must report the outcome via Success or Failure.
func (cb *CircuitBreaker) Allow() bool {
	if cb.maxFailures <= 0 {
		return true
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case stateClosed:
		return true
	case stateOpen:
		if cb.now().Sub(cb.openedAt) >= cb.cooldown {
			// Cooldown elapsed: move to half-open and let one probe through.
			cb.state = stateHalfOpen
			cb.halfOpenBusy = true
			return true
		}
		return false
	case stateHalfOpen:
		// Only a single concurrent probe is permitted.
		if cb.halfOpenBusy {
			return false
		}
		cb.halfOpenBusy = true
		return true
	default:
		return false
	}
}

// Success records a successful call, closing the breaker and clearing failures.
func (cb *CircuitBreaker) Success() {
	if cb.maxFailures <= 0 {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = stateClosed
	cb.failures = 0
	cb.halfOpenBusy = false
}

// Failure records a failed call. In half-open it re-opens immediately; in closed
// it opens once the failure threshold is reached.
func (cb *CircuitBreaker) Failure() {
	if cb.maxFailures <= 0 {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.halfOpenBusy = false
	if cb.state == stateHalfOpen {
		cb.open()
		return
	}
	cb.failures++
	if cb.failures >= cb.maxFailures {
		cb.open()
	}
}

func (cb *CircuitBreaker) open() {
	cb.state = stateOpen
	cb.openedAt = cb.now()
}
