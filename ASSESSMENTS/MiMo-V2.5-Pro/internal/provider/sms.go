package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/notification-service/internal/domain"
)

// SMSProvider simulates an SMS delivery service.
type SMSProvider struct {
	*BaseProvider
}

// NewSMSProvider creates a new simulated SMS provider.
func NewSMSProvider(logger *slog.Logger) *SMSProvider {
	return &SMSProvider{
		BaseProvider: NewBaseProvider(
			"twilio-provider",
			domain.ChannelSMS,
			logger,
			0.08, // 8% failure rate
			100*time.Millisecond,
			300*time.Millisecond,
		),
	}
}

func (p *SMSProvider) Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error {
	p.logger.Info("sending SMS",
		"provider", p.Name(),
		"to", recipient.Value,
	)

	if err := p.simulateWork(ctx); err != nil {
		return fmt.Errorf("SMS delivery failed: %w", err)
	}

	p.logger.Info("SMS sent successfully",
		"provider", p.Name(),
		"to", recipient.Value,
	)

	return nil
}
