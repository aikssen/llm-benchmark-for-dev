package provider

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/notifications-service/internal/domain"
)

type SimulatedProvider struct {
	channel    domain.Channel
	logger     *slog.Logger
	failRate   float64
	latencyMin time.Duration
	latencyMax time.Duration
}

func NewSimulatedProvider(channel domain.Channel, logger *slog.Logger) *SimulatedProvider {
	return &SimulatedProvider{
		channel:    channel,
		logger:     logger,
		failRate:   0.1,
		latencyMin: 50 * time.Millisecond,
		latencyMax: 300 * time.Millisecond,
	}
}

func (p *SimulatedProvider) Channel() domain.Channel {
	return p.channel
}

func (p *SimulatedProvider) Send(ctx context.Context, recipient string, subject string, message string) error {
	latency := p.latencyMin + time.Duration(rand.Int63n(int64(p.latencyMax-p.latencyMin)))

	select {
	case <-time.After(latency):
	case <-ctx.Done():
		return ctx.Err()
	}

	if rand.Float64() < p.failRate {
		p.logger.Warn("simulated provider failure",
			"channel", p.channel,
			"recipient", recipient,
		)
		return &domain.DeliveryError{
			Recipient: recipient,
			Channel:   p.channel,
			Err:       domain.ErrDeliveryFailed,
		}
	}

	p.logger.Info("notification sent",
		"channel", p.channel,
		"recipient", recipient,
		"subject", subject,
	)

	return nil
}
