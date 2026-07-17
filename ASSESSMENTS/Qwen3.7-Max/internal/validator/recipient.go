package validator

import (
	"context"
	"net/mail"
	"regexp"
	"strings"

	"github.com/notifications-service/internal/domain"
)

type RecipientValidator struct{}

func NewRecipientValidator() *RecipientValidator {
	return &RecipientValidator{}
}

func (v *RecipientValidator) Name() string {
	return "recipient"
}

func (v *RecipientValidator) Validate(_ context.Context, recipient string, req *domain.NotificationRequest) error {
	if strings.TrimSpace(recipient) == "" {
		return &domain.ValidationError{
			Field:   "recipient",
			Message: "recipient address cannot be empty",
		}
	}

	switch req.Channel {
	case domain.ChannelEmail:
		if _, err := mail.ParseAddress(recipient); err != nil {
			return &domain.ValidationError{
				Field:   "recipient",
				Message: "invalid email address format",
			}
		}
	case domain.ChannelSMS:
		phoneRegex := regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)
		if !phoneRegex.MatchString(recipient) {
			return &domain.ValidationError{
				Field:   "recipient",
				Message: "invalid phone number format",
			}
		}
	case domain.ChannelPush:
		if len(recipient) < 10 {
			return &domain.ValidationError{
				Field:   "recipient",
				Message: "invalid device token",
			}
		}
	}

	return nil
}
