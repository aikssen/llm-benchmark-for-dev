package providers

import (
	"context"
	"fmt"
	"time"

	"notification-service/internal/domain"
)

// EmailProvider simulates an external email gateway.
type EmailProvider struct {
	name          string
	baseLatency   time.Duration
	failureRate   float64 // 0.0 - 1.0
	maxRetries    int
}

// NewEmailProvider creates a simulated email provider.
func NewEmailProvider() *EmailProvider {
	return &EmailProvider{
		name:        "simulated-email",
		baseLatency: 50 * time.Millisecond,
		failureRate: 0.05,
		maxRetries:  3,
	}
}

func (p *EmailProvider) Name() string            { return p.name }
func (p *EmailProvider) Channel() domain.Channel { return domain.ChannelEmail }

func (p *EmailProvider) Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error {
	return withRetry(ctx, p.maxRetries, func() error {
		if err := simulateProviderCall(ctx, p.baseLatency, p.failureRate); err != nil {
			return fmt.Errorf("email provider error: %w", err)
		}
		return nil
	})
}
