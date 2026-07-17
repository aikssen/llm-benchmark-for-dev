package provider

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"time"

	"notifications/internal/domain"
)

// Simulated is a fake external provider with tunable latency and failure
// behaviour, used for local runs and tests:
//
//   - each call sleeps for a random latency in [MinLatency, MaxLatency]
//     (respecting context cancellation);
//   - each call fails with a temporary error with probability ErrorRate;
//   - recipients containing "undeliverable" always fail with a permanent
//     error (simulates e.g. an unknown address).
//
// All randomness goes through a single mutex-protected RNG so tests can
// inject a deterministic source via NewSimulatedWithRand.
type Simulated struct {
	channel    domain.Channel
	minLatency time.Duration
	maxLatency time.Duration
	errorRate  float64

	mu  sync.Mutex
	rng *rand.Rand
}

// SimulatedConfig tunes a Simulated provider.
type SimulatedConfig struct {
	Channel    domain.Channel
	MinLatency time.Duration
	MaxLatency time.Duration
	ErrorRate  float64 // 0..1 probability of a temporary failure per call
}

// NewSimulated creates a simulated provider with a time-seeded RNG.
func NewSimulated(cfg SimulatedConfig) *Simulated {
	return NewSimulatedWithRand(cfg, rand.New(rand.NewSource(time.Now().UnixNano())))
}

// NewSimulatedWithRand is like NewSimulated but with an explicit RNG,
// which keeps tests deterministic.
func NewSimulatedWithRand(cfg SimulatedConfig, rng *rand.Rand) *Simulated {
	if cfg.MaxLatency < cfg.MinLatency {
		cfg.MaxLatency = cfg.MinLatency
	}
	return &Simulated{
		channel:    cfg.Channel,
		minLatency: cfg.MinLatency,
		maxLatency: cfg.MaxLatency,
		errorRate:  cfg.ErrorRate,
		rng:        rng,
	}
}

func (s *Simulated) Channel() domain.Channel { return s.channel }

func (s *Simulated) Send(ctx context.Context, n *domain.Notification) error {
	latency, fail := s.draw()

	if latency > 0 {
		t := time.NewTimer(latency)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return Temporaryf("send interrupted: %v", ctx.Err())
		case <-t.C:
		}
	}

	// Deterministic permanent-failure hook for demos and tests.
	if strings.Contains(strings.ToLower(n.Recipient), "undeliverable") {
		return Permanentf("recipient %q rejected by %s provider", n.Recipient, s.channel)
	}
	if fail {
		return Temporaryf("%s provider returned an intermittent error", s.channel)
	}
	return nil
}

// draw samples latency and failure atomically.
func (s *Simulated) draw() (latency time.Duration, fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latency = s.minLatency
	if d := s.maxLatency - s.minLatency; d > 0 {
		latency += time.Duration(s.rng.Int63n(int64(d)))
	}
	fail = s.rng.Float64() < s.errorRate
	return latency, fail
}
