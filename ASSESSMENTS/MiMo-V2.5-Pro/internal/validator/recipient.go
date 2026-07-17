package validator

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/notification-service/internal/domain"
	apperrors "github.com/notification-service/pkg/errors"
)

// RecipientValidator validates recipients based on the notification channel.
type RecipientValidator struct{}

// NewRecipientValidator creates a new recipient validator.
func NewRecipientValidator() *RecipientValidator {
	return &RecipientValidator{}
}

func (v *RecipientValidator) Name() string {
	return "recipient"
}

func (v *RecipientValidator) Validate(ctx context.Context, req *domain.CreateNotificationRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if len(req.Recipients) == 0 {
		return &apperrors.ValidationError{
			Field:   "recipients",
			Message: "at least one recipient is required",
		}
	}

	for i, r := range req.Recipients {
		if err := v.validateRecipient(r, req.Channel); err != nil {
			return &apperrors.ValidationError{
				Field:   fmt.Sprintf("recipients[%d]", i),
				Message: err.Error(),
			}
		}
	}

	return nil
}

func (v *RecipientValidator) validateRecipient(r domain.Recipient, channel domain.Channel) error {
	if strings.TrimSpace(r.Value) == "" {
		return fmt.Errorf("recipient value cannot be empty")
	}

	switch channel {
	case domain.ChannelEmail:
		return v.validateEmail(r.Value)
	case domain.ChannelSMS:
		return v.validatePhone(r.Value)
	case domain.ChannelPush:
		return v.validateDeviceToken(r.Value)
	default:
		return fmt.Errorf("unsupported channel: %s", channel)
	}
}

func (v *RecipientValidator) validateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("invalid email address: %s", email)
	}
	return nil
}

func (v *RecipientValidator) validatePhone(phone string) error {
	phoneRegex := regexp.MustCompile(`^\+?[1-9]\d{1,14}$`)
	if !phoneRegex.MatchString(phone) {
		return fmt.Errorf("invalid phone number format: %s", phone)
	}
	return nil
}

func (v *RecipientValidator) validateDeviceToken(token string) error {
	if len(token) < 10 {
		return fmt.Errorf("device token too short")
	}
	return nil
}
