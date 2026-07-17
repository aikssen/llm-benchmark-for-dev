package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/notification-service/internal/domain"
)

// EmailProvider simulates an email delivery service.
type EmailProvider struct {
	*BaseProvider
}

// NewEmailProvider creates a new simulated email provider.
func NewEmailProvider(logger *slog.Logger) *EmailProvider {
	return &EmailProvider{
		BaseProvider: NewBaseProvider(
			"smtp-provider",
			domain.ChannelEmail,
			logger,
			0.05, // 5% failure rate
			50*time.Millisecond,
			200*time.Millisecond,
		),
	}
}

func (p *EmailProvider) Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error {
	p.logger.Info("sending email",
		"provider", p.Name(),
		"to", recipient.Value,
		"subject", content.Subject,
	)

	if err := p.simulateWork(ctx); err != nil {
		return fmt.Errorf("email delivery failed: %w", err)
	}

	p.logger.Info("email sent successfully",
		"provider", p.Name(),
		"to", recipient.Value,
	)

	return nil
}
