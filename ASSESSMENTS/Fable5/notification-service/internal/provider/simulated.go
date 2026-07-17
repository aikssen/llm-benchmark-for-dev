package provider

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"notificationservice/internal/domain"
)

// SimulatedConfig controls the failure and latency behavior of a Simulated
// provider so the unreliability of real providers can be reproduced locally.
type SimulatedConfig struct {
	// MinLatency and MaxLatency bound the artificial per-send latency.
	MinLatency time.Duration
	MaxLatency time.Duration
	// TransientFailureRate is the probability [0,1] of a retryable failure
	// (timeouts, 5xx, throttling).
	TransientFailureRate float64
	// PermanentFailureRate is the probability [0,1] of a non-retryable
	// failure (hard bounce, recipient rejected).
	PermanentFailureRate float64
}

// Simulated is a fake provider that mimics an unreliable external system:
// variable latency, intermittent transient errors and occasional permanent
// rejections.
type Simulated struct {
	name    string
	channel domain.Channel
	cfg     SimulatedConfig
	logger  *slog.Logger
	rand    func() float64 // injectable for deterministic tests
}

var _ Provider = (*Simulated)(nil)

// NewSimulated builds a simulated provider for the given channel.
func NewSimulated(channel domain.Channel, cfg SimulatedConfig, logger *slog.Logger) *Simulated {
	return &Simulated{
		name:    "sim-" + string(channel),
		channel: channel,
		cfg:     cfg,
		logger:  logger,
		rand:    rand.Float64,
	}
}

func (s *Simulated) Name() string            { return s.name }
func (s *Simulated) Channel() domain.Channel { return s.channel }

func (s *Simulated) Send(ctx context.Context, n domain.Notification, recipient string) error {
	if err := s.sleep(ctx); err != nil {
		return err // cancelled/timed out mid-call; treated as transient
	}

	roll := s.rand()
	switch {
	case roll < s.cfg.PermanentFailureRate:
		return Permanent(fmt.Errorf("%s: recipient rejected by provider", s.name))
	case roll < s.cfg.PermanentFailureRate+s.cfg.TransientFailureRate:
		return fmt.Errorf("%s: provider temporarily unavailable", s.name)
	}

	s.logger.Info("simulated delivery sent",
		"provider", s.name,
		"notification_id", n.ID,
		"recipient", recipient,
	)
	return nil
}

// sleep waits for a random duration within the configured latency bounds,
// aborting early if ctx is done.
func (s *Simulated) sleep(ctx context.Context) error {
	d := s.cfg.MinLatency
	if spread := s.cfg.MaxLatency - s.cfg.MinLatency; spread > 0 {
		d += time.Duration(s.rand() * float64(spread))
	}
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: send aborted: %w", s.name, ctx.Err())
	case <-timer.C:
		return nil
	}
}
