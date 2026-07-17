package provider

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// SimulatorConfig models the unreliability of an external provider.
type SimulatorConfig struct {
	Name          string
	FailureProb   float64       // probability [0,1] of a transient failure
	SlowProb      float64       // probability [0,1] of a slow response
	SlowDelay     time.Duration // delay applied on slow responses
	FailureIfSlow bool          // if true, slow responses also fail (simulating timeouts)
	MinDelay      time.Duration // baseline latency applied to every call
	Rand          *rand.Rand    // injectable for deterministic tests
}

// Simulator is a Provider that mimics an unreliable external system. It is safe
// for concurrent use.
type Simulator struct {
	cfg      SimulatorConfig
	mu       sync.Mutex
	randMu   sync.Mutex
	attempts int
}

// NewSimulator builds a simulator provider from the given config.
func NewSimulator(cfg SimulatorConfig) *Simulator {
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
	}
	return &Simulator{cfg: cfg}
}

func (s *Simulator) Name() string { return s.cfg.Name }

// Send simulates latency and probabilistic failures, respecting ctx.
func (s *Simulator) Send(ctx context.Context, out Outbound) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.attempts++
	s.mu.Unlock()

	delay := s.cfg.MinDelay
	if s.roll(s.cfg.SlowProb) {
		delay += s.cfg.SlowDelay
	}
	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	if s.cfg.FailureIfSlow && delay > s.cfg.MinDelay {
		return ErrSimulatedTimeout
	}
	if s.roll(s.cfg.FailureProb) {
		return ErrSimulatedTransient
	}
	return nil
}

// roll returns true with probability p, thread-safe.
func (s *Simulator) roll(p float64) bool {
	if p <= 0 {
		return false
	}
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.cfg.Rand.Float64() < p
}

// Attempts returns the total number of Send attempts observed.
func (s *Simulator) Attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}
