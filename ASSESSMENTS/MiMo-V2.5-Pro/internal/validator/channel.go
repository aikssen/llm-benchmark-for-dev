package validator

import (
	"context"

	"github.com/notification-service/internal/domain"
	apperrors "github.com/notification-service/pkg/errors"
)

// ChannelValidator validates that the notification channel is supported.
type ChannelValidator struct{}

// NewChannelValidator creates a new channel validator.
func NewChannelValidator() *ChannelValidator {
	return &ChannelValidator{}
}

func (v *ChannelValidator) Name() string {
	return "channel"
}

func (v *ChannelValidator) Validate(ctx context.Context, req *domain.CreateNotificationRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if !req.Channel.IsValid() {
		return &apperrors.ValidationError{
			Field:   "channel",
			Message: "unsupported channel: " + string(req.Channel),
		}
	}

	return nil
}
