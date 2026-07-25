package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the breaker is refusing calls.
//
// It is deliberately *not* retryable: once a provider is known to be down,
// burning the delivery's retry budget on immediate re-attempts helps nobody.
// The delivery fails fast and is recorded with a clear reason.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// State is the breaker's current mode.
type State string

const (
	// StateClosed: calls flow through normally.
	StateClosed State = "closed"
	// StateOpen: calls are rejected immediately.
	StateOpen State = "open"
	// StateHalfOpen: a limited number of probe calls are allowed through to
	// discover whether the provider has recovered.
	StateHalfOpen State = "half_open"
)

// BreakerConfig configures a CircuitBreaker.
type BreakerConfig struct {
	// FailureThreshold is the number of consecutive failures that trips the
	// breaker. Zero or less disables the breaker.
	FailureThreshold int
	// OpenDuration is how long the breaker stays open before probing.
	OpenDuration time.Duration
	// HalfOpenMaxCalls is how many probes are allowed at once while
	// half-open. Defaults to 1.
	HalfOpenMaxCalls int
	// SuccessThreshold is the number of successful probes needed to close
	// the breaker again. Defaults to HalfOpenMaxCalls.
	SuccessThreshold int
}

// DefaultBreakerConfig is a reasonable starting point for an HTTP provider.
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		FailureThreshold: 5,
		OpenDuration:     5 * time.Second,
		HalfOpenMaxCalls: 2,
		SuccessThreshold: 2,
	}
}

// CircuitBreaker guards one downstream dependency.
//
// Only failures the caller classifies as *transient* count towards tripping
// it: a provider rejecting a malformed address is a problem with the message,
// not with the provider, and must not take the channel down for everyone.
type CircuitBreaker struct {
	name string
	cfg  BreakerConfig
	now  func() time.Time

	mu                  sync.Mutex
	state               State
	consecutiveFailures int
	halfOpenInFlight    int
	halfOpenSuccesses   int
	openedAt            time.Time
}

// BreakerOption customises a CircuitBreaker.
type BreakerOption func(*CircuitBreaker)

// WithBreakerClock replaces the time source, so tests need not sleep.
func WithBreakerClock(now func() time.Time) BreakerOption {
	return func(b *CircuitBreaker) { b.now = now }
}

// NewCircuitBreaker builds a breaker named after the dependency it guards.
func NewCircuitBreaker(name string, cfg BreakerConfig, opts ...BreakerOption) *CircuitBreaker {
	if cfg.HalfOpenMaxCalls <= 0 {
		cfg.HalfOpenMaxCalls = 1
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = cfg.HalfOpenMaxCalls
	}
	b := &CircuitBreaker{name: name, cfg: cfg, now: time.Now, state: StateClosed}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Name returns the guarded dependency's name.
func (b *CircuitBreaker) Name() string { return b.name }

// State returns the current state, applying any pending open->half-open
// transition first.
func (b *CircuitBreaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshLocked()
	return b.state
}

// Execute runs fn unless the breaker is open.
//
// isTransient decides whether a given error counts against the breaker. It
// may be nil, in which case every error counts.
func (b *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error, isTransient func(error) bool) error {
	if b.cfg.FailureThreshold <= 0 {
		return fn(ctx) // breaker disabled
	}
	if err := b.acquire(); err != nil {
		return err
	}

	err := fn(ctx)
	switch {
	case err == nil:
		b.onSuccess()
	case isTransient == nil || isTransient(err):
		b.onFailure()
	default:
		// A permanent, message-specific failure. The dependency answered, so
		// release the half-open slot without counting it either way.
		b.onIrrelevant()
	}
	return err
}

// acquire admits a call, or rejects it with ErrCircuitOpen.
func (b *CircuitBreaker) acquire() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshLocked()

	switch b.state {
	case StateOpen:
		return ErrCircuitOpen
	case StateHalfOpen:
		if b.halfOpenInFlight >= b.cfg.HalfOpenMaxCalls {
			return ErrCircuitOpen
		}
		b.halfOpenInFlight++
	}
	return nil
}

// refreshLocked moves an expired open breaker into half-open.
func (b *CircuitBreaker) refreshLocked() {
	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cfg.OpenDuration {
		b.state = StateHalfOpen
		b.halfOpenInFlight = 0
		b.halfOpenSuccesses = 0
	}
}

func (b *CircuitBreaker) onSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveFailures = 0
	if b.state == StateHalfOpen {
		b.halfOpenInFlight--
		b.halfOpenSuccesses++
		if b.halfOpenSuccesses >= b.cfg.SuccessThreshold {
			b.state = StateClosed
			b.halfOpenSuccesses = 0
			b.halfOpenInFlight = 0
		}
	}
}

func (b *CircuitBreaker) onFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateHalfOpen:
		// The provider is still unhealthy: go straight back to open.
		b.tripLocked()
	case StateClosed:
		b.consecutiveFailures++
		if b.consecutiveFailures >= b.cfg.FailureThreshold {
			b.tripLocked()
		}
	}
}

// onIrrelevant releases a half-open slot for an error that says nothing about
// the dependency's health.
func (b *CircuitBreaker) onIrrelevant() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateHalfOpen && b.halfOpenInFlight > 0 {
		b.halfOpenInFlight--
	}
}

func (b *CircuitBreaker) tripLocked() {
	b.state = StateOpen
	b.openedAt = b.now()
	b.consecutiveFailures = 0
	b.halfOpenInFlight = 0
	b.halfOpenSuccesses = 0
}
