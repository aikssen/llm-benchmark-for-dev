package provider

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/assessment/notify-service/internal/domain"
)

func NewEmailProvider() domain.Provider {
	return &simulatedProvider{
		name:         domain.ChannelEmail,
		baseLatency:  50 * time.Millisecond,
		failureRate:  0.1,
	}
}

func NewSMSProvider() domain.Provider {
	return &simulatedProvider{
		name:         domain.ChannelSMS,
		baseLatency:  80 * time.Millisecond,
		failureRate:  0.15,
	}
}

func NewPushProvider() domain.Provider {
	return &simulatedProvider{
		name:         domain.ChannelPush,
		baseLatency:  30 * time.Millisecond,
		failureRate:  0.1,
	}
}

type simulatedProvider struct {
	name         domain.Channel
	baseLatency  time.Duration
	failureRate  float64
}

func (p *simulatedProvider) Name() domain.Channel {
	return p.name
}

func (p *simulatedProvider) Send(_ context.Context, recipient domain.Recipient, message domain.Message) error {
	jitter := time.Duration(rand.Int63n(int64(p.baseLatency)))
	time.Sleep(p.baseLatency + jitter)

	if rand.Float64() < p.failureRate {
		return fmt.Errorf("simulated %s delivery failure to %s", p.name, recipient.ID)
	}

	return nil
}
