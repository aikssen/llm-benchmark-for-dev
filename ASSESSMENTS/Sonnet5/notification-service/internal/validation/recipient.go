package validation

import (
	"context"
	"fmt"
	"regexp"

	"github.com/aikssen/notification-service/internal/domain"
)

var (
	emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	// E.164: optional leading +, up to 15 digits.
	phoneRe = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)
)

// FormatValidator checks that a recipient address is syntactically valid
// for the target channel. It runs first among recipient-level validators
// because it is pure/local (a regex match), unlike preference or
// rate-limit checks which simulate a lookup against another system.
type FormatValidator struct{}

func (FormatValidator) Name() string { return "recipient_format" }

func (FormatValidator) Validate(_ context.Context, channel domain.Channel, recipient string) error {
	if recipient == "" {
		return fmt.Errorf("%w: recipient must not be empty", domain.ErrValidation)
	}
	switch channel {
	case domain.ChannelEmail:
		if !emailRe.MatchString(recipient) {
			return fmt.Errorf("%w: %q is not a valid email address", domain.ErrValidation, recipient)
		}
	case domain.ChannelSMS:
		if !phoneRe.MatchString(recipient) {
			return fmt.Errorf("%w: %q is not a valid E.164 phone number", domain.ErrValidation, recipient)
		}
	case domain.ChannelPush:
		if len(recipient) < 8 {
			return fmt.Errorf("%w: %q is not a valid device token", domain.ErrValidation, recipient)
		}
	default:
		return fmt.Errorf("%w: unsupported channel %q", domain.ErrValidation, channel)
	}
	return nil
}
