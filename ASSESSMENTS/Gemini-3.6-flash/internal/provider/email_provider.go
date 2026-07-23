package provider

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"notification-service/internal/domain"
)

type SimulatedEmailProvider struct {
	id          string
	failureRate float64 // 0.0 to 1.0
	latency     time.Duration
	sentCount   uint64
}

func NewSimulatedEmailProvider(id string, failureRate float64, latency time.Duration) *SimulatedEmailProvider {
	return &SimulatedEmailProvider{
		id:          id,
		failureRate: failureRate,
		latency:     latency,
	}
}

func (p *SimulatedEmailProvider) ID() string {
	return p.id
}

func (p *SimulatedEmailProvider) Channel() domain.Channel {
	return domain.ChannelEmail
}

func (p *SimulatedEmailProvider) SentCount() uint64 {
	return atomic.LoadUint64(&p.sentCount)
}

func (p *SimulatedEmailProvider) Send(ctx context.Context, item *domain.SingleRecipientItem) error {
	if p.latency > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.latency):
		}
	}

	if p.failureRate > 0 && rand.Float64() < p.failureRate {
		return fmt.Errorf("simulated email provider '%s' intermittent error", p.id)
	}

	atomic.AddUint64(&p.sentCount, 1)
	return nil
}
