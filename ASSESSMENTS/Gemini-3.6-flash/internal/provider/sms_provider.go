package provider

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"notification-service/internal/domain"
)

type SimulatedSMSProvider struct {
	id          string
	failureRate float64
	latency     time.Duration
	sentCount   uint64
}

func NewSimulatedSMSProvider(id string, failureRate float64, latency time.Duration) *SimulatedSMSProvider {
	return &SimulatedSMSProvider{
		id:          id,
		failureRate: failureRate,
		latency:     latency,
	}
}

func (p *SimulatedSMSProvider) ID() string {
	return p.id
}

func (p *SimulatedSMSProvider) Channel() domain.Channel {
	return domain.ChannelSMS
}

func (p *SimulatedSMSProvider) SentCount() uint64 {
	return atomic.LoadUint64(&p.sentCount)
}

func (p *SimulatedSMSProvider) Send(ctx context.Context, item *domain.SingleRecipientItem) error {
	if p.latency > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.latency):
		}
	}

	if p.failureRate > 0 && rand.Float64() < p.failureRate {
		return fmt.Errorf("simulated SMS provider '%s' intermittent error", p.id)
	}

	atomic.AddUint64(&p.sentCount, 1)
	return nil
}
