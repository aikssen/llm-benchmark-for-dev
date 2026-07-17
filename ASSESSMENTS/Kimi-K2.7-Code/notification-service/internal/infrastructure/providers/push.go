package providers

import (
	"context"
	"fmt"
	"time"

	"notification-service/internal/domain"
)

// PushProvider simulates an external push notification gateway.
type PushProvider struct {
	name          string
	baseLatency   time.Duration
	failureRate   float64
	maxRetries    int
}

// NewPushProvider creates a simulated push provider.
func NewPushProvider() *PushProvider {
	return &PushProvider{
		name:        "simulated-push",
		baseLatency: 30 * time.Millisecond,
		failureRate: 0.03,
		maxRetries:  2,
	}
}

func (p *PushProvider) Name() string            { return p.name }
func (p *PushProvider) Channel() domain.Channel { return domain.ChannelPush }

func (p *PushProvider) Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error {
	return withRetry(ctx, p.maxRetries, func() error {
		if err := simulateProviderCall(ctx, p.baseLatency, p.failureRate); err != nil {
			return fmt.Errorf("push provider error: %w", err)
		}
		return nil
	})
}
