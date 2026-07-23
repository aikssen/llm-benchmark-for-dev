package validator

import (
	"context"
	"fmt"
	"strings"

	"notification-service/internal/domain"
)

type ContentValidator struct {
	MaxSMSLength   int
	MaxEmailLength int
	MaxPushLength  int
}

func NewContentValidator() *ContentValidator {
	return &ContentValidator{
		MaxSMSLength:   160,
		MaxEmailLength: 10000,
		MaxPushLength:  4000,
	}
}

func (cv *ContentValidator) Name() string {
	return "ContentValidator"
}

func (cv *ContentValidator) Validate(ctx context.Context, item *domain.SingleRecipientItem) error {
	body := strings.TrimSpace(item.Body)
	if body == "" {
		return domain.ErrEmptyContent
	}

	switch item.Channel.Normalized() {
	case domain.ChannelSMS:
		if len(body) > cv.MaxSMSLength {
			return fmt.Errorf("%w: SMS body exceeds max length of %d chars", domain.ErrValidationFailed, cv.MaxSMSLength)
		}
	case domain.ChannelEmail:
		if len(body) > cv.MaxEmailLength {
			return fmt.Errorf("%w: Email body exceeds max length of %d chars", domain.ErrValidationFailed, cv.MaxEmailLength)
		}
	case domain.ChannelPush:
		if len(body) > cv.MaxPushLength {
			return fmt.Errorf("%w: Push body exceeds max length of %d chars", domain.ErrValidationFailed, cv.MaxPushLength)
		}
	}

	return nil
}
