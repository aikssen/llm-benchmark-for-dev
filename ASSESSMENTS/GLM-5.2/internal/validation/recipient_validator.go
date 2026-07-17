package validation

import (
	"context"
	"net/mail"
	"regexp"

	"github.com/example/notification-service/internal/domain"
)

// recipientValidator checks that the recipient has a valid address for the
// notification's channel.
type recipientValidator struct{}

// NewRecipientValidator returns a validator that verifies the channel-specific
// recipient address.
func NewRecipientValidator() Validator { return recipientValidator{} }

func (recipientValidator) Name() string { return "recipient" }

func (recipientValidator) Validate(ctx context.Context, in Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch in.Notification.Channel {
	case domain.ChannelEmail:
		return validateEmail(in.Recipient)
	case domain.ChannelSMS:
		return validatePhone(in.Recipient)
	case domain.ChannelPush:
		return validatePushToken(in.Recipient)
	default:
		return domain.NewValidationError("unsupported_channel", "channel", "channel is not supported")
	}
}

func validateEmail(r domain.Recipient) error {
	if r.Email == "" {
		return domain.NewValidationError("missing_recipient_address", "email", "recipient has no email address")
	}
	if _, err := mail.ParseAddress(r.Email); err != nil {
		return domain.NewValidationError("invalid_email", "email", "email address is not valid")
	}
	return nil
}

var phoneRe = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)

func validatePhone(r domain.Recipient) error {
	if r.Phone == "" {
		return domain.NewValidationError("missing_recipient_address", "phone", "recipient has no phone number")
	}
	if !phoneRe.MatchString(r.Phone) {
		return domain.NewValidationError("invalid_phone", "phone", "phone number is not valid (E.164-like expected)")
	}
	return nil
}

func validatePushToken(r domain.Recipient) error {
	if r.PushToken == "" {
		return domain.NewValidationError("missing_recipient_address", "push_token", "recipient has no push token")
	}
	if len(r.PushToken) > 4096 {
		return domain.NewValidationError("invalid_push_token", "push_token", "push token is too long")
	}
	return nil
}
