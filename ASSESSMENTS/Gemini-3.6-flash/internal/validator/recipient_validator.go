package validator

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"notification-service/internal/domain"
)

var (
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	phoneRegex = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)
)

type RecipientValidator struct{}

func NewRecipientValidator() *RecipientValidator {
	return &RecipientValidator{}
}

func (rv *RecipientValidator) Name() string {
	return "RecipientValidator"
}

func (rv *RecipientValidator) Validate(ctx context.Context, item *domain.SingleRecipientItem) error {
	recipient := strings.TrimSpace(item.Recipient)
	if recipient == "" {
		return fmt.Errorf("%w: recipient cannot be empty", domain.ErrInvalidRecipient)
	}

	switch item.Channel.Normalized() {
	case domain.ChannelEmail:
		if !emailRegex.MatchString(recipient) {
			return fmt.Errorf("%w: invalid email address '%s'", domain.ErrInvalidRecipient, recipient)
		}
	case domain.ChannelSMS:
		if !phoneRegex.MatchString(recipient) {
			return fmt.Errorf("%w: invalid phone number '%s'", domain.ErrInvalidRecipient, recipient)
		}
	case domain.ChannelPush:
		if len(recipient) < 8 {
			return fmt.Errorf("%w: push token too short '%s'", domain.ErrInvalidRecipient, recipient)
		}
	default:
		return domain.ErrInvalidChannel
	}

	return nil
}
