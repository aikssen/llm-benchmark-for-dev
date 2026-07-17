package validator

import (
	"context"
	"strings"

	"github.com/notifications-service/internal/domain"
)

const maxMessageLength = 10000

type ContentValidator struct{}

func NewContentValidator() *ContentValidator {
	return &ContentValidator{}
}

func (v *ContentValidator) Name() string {
	return "content"
}

func (v *ContentValidator) Validate(_ context.Context, _ string, req *domain.NotificationRequest) error {
	if strings.TrimSpace(req.Message) == "" {
		return &domain.ValidationError{
			Field:   "message",
			Message: "message cannot be empty",
		}
	}

	if len(req.Message) > maxMessageLength {
		return &domain.ValidationError{
			Field:   "message",
			Message: "message exceeds maximum length",
		}
	}

	if req.Channel == domain.ChannelEmail && strings.TrimSpace(req.Subject) == "" {
		return &domain.ValidationError{
			Field:   "subject",
			Message: "subject is required for email notifications",
		}
	}

	return nil
}
