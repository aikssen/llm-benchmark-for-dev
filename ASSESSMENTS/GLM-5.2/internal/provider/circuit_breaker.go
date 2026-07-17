package provider

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CircuitBreakerOptions configures the circuit breaker.
type CircuitBreakerOptions struct {
	// FailureThreshold is the number of consecutive failures that opens the
	// circuit.
	FailureThreshold int
	// Cooldown is how long the circuit stays open before transitioning to
	// half-open (a single probe request).
	Cooldown time.Duration
	// Now is injectable for testing.
	Now func() time.Time
}

// breakerState enumerates the circuit breaker states.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// WithCircuitBreaker wraps a provider with a circuit breaker that prevents
// hammering an unavailable provider. After FailureThreshold consecutive
// failures the circuit opens for Cooldown; a single probe is then allowed
// (half-open), and on success the circuit closes again.
func WithCircuitBreaker(opts CircuitBreakerOptions) Middleware {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return func(next Provider) Provider {
		return &circuitBreaker{
			next: next,
			opts: opts,
		}
	}
}

type circuitBreaker struct {
	next Provider
	opts CircuitBreakerOptions

	mu       sync.Mutex
	state    breakerState
	failures int
	openedAt time.Time
}

func (c *circuitBreaker) Name() string { return c.next.Name() }

// ErrCircuitOpen is returned when the circuit is open and no probe is due.
var ErrCircuitOpen = errors.New("provider circuit open")

func (c *circuitBreaker) Send(ctx context.Context, out Outbound) error {
	if err := c.before(); err != nil {
		return err
	}
	err := c.next.Send(ctx, out)
	c.after(err)
	return err
}

// before determines whether a request may proceed and transitions open ->
// half-open once the cooldown elapsed.
func (c *circuitBreaker) before() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case breakerClosed:
		return nil
	case breakerOpen:
		if c.opts.Now().Sub(c.openedAt) >= c.opts.Cooldown {
			c.state = breakerHalfOpen
			return nil
		}
		return ErrCircuitOpen
	case breakerHalfOpen:
		// Only one probe at a time while half-open.
		return ErrCircuitOpen
	}
	return nil
}

func (c *circuitBreaker) after(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.failures = 0
		c.state = breakerClosed
		return
	}
	c.failures++
	if c.state == breakerHalfOpen {
		// Probe failed: re-open.
		c.state = breakerOpen
		c.openedAt = c.opts.Now()
		return
	}
	if c.failures >= c.opts.FailureThreshold {
		c.state = breakerOpen
		c.openedAt = c.opts.Now()
	}
}

// State exposes the current state as a string for observability/testing.
func (c *circuitBreaker) State() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case breakerClosed:
		return "closed"
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	}
	return "unknown"
}
