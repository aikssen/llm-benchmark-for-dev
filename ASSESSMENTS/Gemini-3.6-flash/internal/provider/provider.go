package provider

import (
	"context"

	"notification-service/internal/domain"
)

// Provider interface defines the contract for external communication channels.
type Provider interface {
	ID() string
	Channel() domain.Channel
	Send(ctx context.Context, item *domain.SingleRecipientItem) error
}
