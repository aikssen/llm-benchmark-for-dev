package resilience

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type State string

const (
	StateClosed   State = "CLOSED"
	StateOpen     State = "OPEN"
	StateHalfOpen State = "HALF_OPEN"
)

type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failureThreshold int
	successThreshold int
	resetTimeout     time.Duration
	failureCount     int
	successCount     int
	lastStateChange  time.Time
}

func NewCircuitBreaker(failureThreshold, successThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		resetTimeout:     resetTimeout,
		lastStateChange:  time.Now(),
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	now := time.Now()

	if cb.state == StateOpen {
		if now.Sub(cb.lastStateChange) >= cb.resetTimeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			cb.failureCount = 0
			cb.lastStateChange = now
		} else {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
	}
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		if cb.state == StateHalfOpen || cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
			cb.lastStateChange = time.Now()
		}
		return err
	}

	if cb.state == StateHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.lastStateChange = time.Now()
		}
	} else if cb.state == StateClosed {
		cb.failureCount = 0
	}

	return nil
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
