package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/notification-service/internal/domain"
)

// PushProvider simulates a push notification service.
type PushProvider struct {
	*BaseProvider
}

// NewPushProvider creates a new simulated push notification provider.
func NewPushProvider(logger *slog.Logger) *PushProvider {
	return &PushProvider{
		BaseProvider: NewBaseProvider(
			"fcm-provider",
			domain.ChannelPush,
			logger,
			0.10, // 10% failure rate
			30*time.Millisecond,
			150*time.Millisecond,
		),
	}
}

func (p *PushProvider) Send(ctx context.Context, recipient domain.Recipient, content domain.Content) error {
	p.logger.Info("sending push notification",
		"provider", p.Name(),
		"to", recipient.Value,
	)

	if err := p.simulateWork(ctx); err != nil {
		return fmt.Errorf("push delivery failed: %w", err)
	}

	p.logger.Info("push notification sent successfully",
		"provider", p.Name(),
		"to", recipient.Value,
	)

	return nil
}
