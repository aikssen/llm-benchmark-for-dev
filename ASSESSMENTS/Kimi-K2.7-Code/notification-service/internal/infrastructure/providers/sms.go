package providers

import (
	"context"
	"fmt"
	"time"

	"notification-service/internal/domain"
)

// SMSProvider simulates an external SMS gateway.
type SMSProvider struct {
	name          string
	baseLatency   time.Duration
	failureRate   float64
	maxRetries    int
}

// NewSMSProvider creates a simulated SMS provider.
func NewSMSProvider() *SMSProvider {
	return &SMSProvider{
		name:        "simulated-sms",
		baseLatency: 80 * time.Millisecond,
		failureRate: 0.10,
		maxRetries:  3,
	}
}

func (p *SMSProvider) Name() string            { return p.name }
func (p *SMSProvider) Channel() domain.Channel { return domain.ChannelSMS }

func (p *SMSProvider) Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error {
	return withRetry(ctx, p.maxRetries, func() error {
		if err := simulateProviderCall(ctx, p.baseLatency, p.failureRate); err != nil {
			return fmt.Errorf("sms provider error: %w", err)
		}
		return nil
	})
}
