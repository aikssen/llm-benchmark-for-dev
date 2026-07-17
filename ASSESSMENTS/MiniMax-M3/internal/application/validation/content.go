package validation

import (
	"context"
	"strings"

	"github.com/anomalyco/notification-service/internal/domain"
)

type ContentValidator struct{}

func (ContentValidator) Name() string { return "content" }

func (ContentValidator) Validate(_ context.Context, n *domain.Notification) error {
	if err := domain.ValidateRecipient(n.Channel, n.Recipient); err != nil {
		return err
	}
	if err := domain.ValidateMessage(n.Channel, n.Message); err != nil {
		return err
	}
	if strings.TrimSpace(n.Message.Body) == "" {
		return domain.NewValidationError("message.body", "must not be empty")
	}
	return nil
}
