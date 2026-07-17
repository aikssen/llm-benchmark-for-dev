package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/notification-service/internal/domain"
	apperrors "github.com/notification-service/pkg/errors"
)

// ContentValidator validates the message content.
type ContentValidator struct {
	maxBodyLength int
}

// NewContentValidator creates a new content validator.
func NewContentValidator(maxBodyLength int) *ContentValidator {
	return &ContentValidator{
		maxBodyLength: maxBodyLength,
	}
}

func (v *ContentValidator) Name() string {
	return "content"
}

func (v *ContentValidator) Validate(ctx context.Context, req *domain.CreateNotificationRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if strings.TrimSpace(req.Content.Body) == "" {
		return &apperrors.ValidationError{
			Field:   "content.body",
			Message: "message body cannot be empty",
		}
	}

	if len(req.Content.Body) > v.maxBodyLength {
		return &apperrors.ValidationError{
			Field:   "content.body",
			Message: fmt.Sprintf("message body exceeds maximum length of %d characters", v.maxBodyLength),
		}
	}

	if req.Channel == domain.ChannelEmail && strings.TrimSpace(req.Content.Subject) == "" {
		return &apperrors.ValidationError{
			Field:   "content.subject",
			Message: "subject is required for email notifications",
		}
	}

	return nil
}
