package provider

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"notification-service/internal/domain"
)

type SimulatedPushProvider struct {
	id          string
	failureRate float64
	latency     time.Duration
	sentCount   uint64
}

func NewSimulatedPushProvider(id string, failureRate float64, latency time.Duration) *SimulatedPushProvider {
	return &SimulatedPushProvider{
		id:          id,
		failureRate: failureRate,
		latency:     latency,
	}
}

func (p *SimulatedPushProvider) ID() string {
	return p.id
}

func (p *SimulatedPushProvider) Channel() domain.Channel {
	return domain.ChannelPush
}

func (p *SimulatedPushProvider) SentCount() uint64 {
	return atomic.LoadUint64(&p.sentCount)
}

func (p *SimulatedPushProvider) Send(ctx context.Context, item *domain.SingleRecipientItem) error {
	if p.latency > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.latency):
		}
	}

	if p.failureRate > 0 && rand.Float64() < p.failureRate {
		return fmt.Errorf("simulated Push provider '%s' intermittent error", p.id)
	}

	atomic.AddUint64(&p.sentCount, 1)
	return nil
}
