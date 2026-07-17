package provider

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

// Simulated stands in for a real third-party gateway (SendGrid, Twilio,
// FCM, ...). It fabricates realistic unreliability — variable latency and
// a configurable failure rate — so the delivery pipeline's resilience
// (retries, circuit breaker) can be exercised without any real network
// calls. It honors context cancellation/timeout like a real HTTP client
// would.
type Simulated struct {
	channel     domain.Channel
	failureRate float64
	minLatency  time.Duration
	maxLatency  time.Duration
}

// NewSimulated builds a simulated provider for channel. failureRate is the
// probability (0..1) that an otherwise-successful call fails; latency is
// drawn uniformly from [minLatency, maxLatency].
func NewSimulated(channel domain.Channel, failureRate float64, minLatency, maxLatency time.Duration) *Simulated {
	return &Simulated{
		channel:     channel,
		failureRate: failureRate,
		minLatency:  minLatency,
		maxLatency:  maxLatency,
	}
}

func (s *Simulated) Channel() domain.Channel { return s.channel }

func (s *Simulated) Send(ctx context.Context, n domain.Notification) error {
	latency := s.minLatency
	if s.maxLatency > s.minLatency {
		latency += time.Duration(rand.Int64N(int64(s.maxLatency - s.minLatency)))
	}

	timer := time.NewTimer(latency)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("%s provider: %w", s.channel, ctx.Err())
	case <-timer.C:
	}

	if rand.Float64() < s.failureRate {
		return fmt.Errorf("%s provider: simulated delivery failure for recipient %s", s.channel, n.Recipient)
	}
	return nil
}
