package provider

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/notification-service/internal/domain"
	apperrors "github.com/notification-service/pkg/errors"
)

// BaseProvider provides common functionality for simulated providers.
type BaseProvider struct {
	name        string
	channel     domain.Channel
	logger      *slog.Logger
	failureRate float64 // 0.0 to 1.0, probability of simulated failure
	minLatency  time.Duration
	maxLatency  time.Duration
}

// NewBaseProvider creates a new base provider.
func NewBaseProvider(
	name string,
	channel domain.Channel,
	logger *slog.Logger,
	failureRate float64,
	minLatency, maxLatency time.Duration,
) *BaseProvider {
	return &BaseProvider{
		name:        name,
		channel:     channel,
		logger:      logger,
		failureRate: failureRate,
		minLatency:  minLatency,
		maxLatency:  maxLatency,
	}
}

func (p *BaseProvider) Channel() domain.Channel {
	return p.channel
}

func (p *BaseProvider) Name() string {
	return p.name
}

// simulateWork simulates network latency and potential failures.
func (p *BaseProvider) simulateWork(ctx context.Context) error {
	latency := p.minLatency + time.Duration(rand.Int63n(int64(p.maxLatency-p.minLatency)))

	select {
	case <-time.After(latency):
	case <-ctx.Done():
		return ctx.Err()
	}

	if rand.Float64() < p.failureRate {
		return &apperrors.ProviderError{
			Provider: p.name,
			Message:  "simulated transient failure",
		}
	}

	return nil
}
