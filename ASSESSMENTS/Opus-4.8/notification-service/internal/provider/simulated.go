package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

// SimConfig controls the simulated failure behaviour of a provider so we can
// exercise the reliability machinery (retries, timeouts, circuit breaker)
// without real external systems.
type SimConfig struct {
	// FailureRate is the probability [0,1] that a send returns a transient
	// (retryable) error.
	FailureRate float64
	// PermanentRate is the probability [0,1] that a send returns a permanent
	// (non-retryable) error. Evaluated after FailureRate.
	PermanentRate float64
	// MinLatency and MaxLatency bound the simulated processing time. A latency
	// that exceeds the caller's context deadline surfaces as a timeout.
	MinLatency time.Duration
	MaxLatency time.Duration
}

// SimulatedProvider is a configurable, concurrency-safe fake provider. It models
// the failure modes the assessment calls out: slow responses, intermittent
// errors and hard failures. math/rand/v2's top-level functions are safe for
// concurrent use, so no locking is needed here.
type SimulatedProvider struct {
	channel domain.Channel
	cfg     SimConfig
	log     *slog.Logger
}

// NewSimulated returns a simulated provider for the given channel.
func NewSimulated(channel domain.Channel, cfg SimConfig, log *slog.Logger) *SimulatedProvider {
	if log == nil {
		log = slog.Default()
	}
	return &SimulatedProvider{channel: channel, cfg: cfg, log: log}
}

func (p *SimulatedProvider) Channel() domain.Channel { return p.channel }

func (p *SimulatedProvider) Send(ctx context.Context, n *domain.Notification) error {
	// Simulate variable provider latency, but remain responsive to context
	// cancellation so the delivery layer's per-attempt timeout can cut a slow
	// provider short.
	if delay := p.latency(); delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return Retryable(fmt.Errorf("%s provider: %w", p.channel, ctx.Err()))
		}
	}

	// Respect a deadline that may have elapsed during latency simulation.
	if err := ctx.Err(); err != nil {
		return Retryable(fmt.Errorf("%s provider: %w", p.channel, err))
	}

	r := rand.Float64()
	switch {
	case r < p.cfg.FailureRate:
		return Retryable(fmt.Errorf("%s provider: temporary upstream error", p.channel))
	case r < p.cfg.FailureRate+p.cfg.PermanentRate:
		return Permanent(fmt.Errorf("%s provider: %w", p.channel, errRejected))
	default:
		p.log.Debug("notification delivered",
			slog.String("provider", string(p.channel)),
			slog.String("notification_id", n.ID),
			slog.String("recipient", n.Recipient),
		)
		return nil
	}
}

// latency returns a random duration within the configured bounds.
func (p *SimulatedProvider) latency() time.Duration {
	lo, hi := p.cfg.MinLatency, p.cfg.MaxLatency
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(rand.Int64N(int64(hi-lo)))
}

var errRejected = errors.New("permanently rejected by upstream")
