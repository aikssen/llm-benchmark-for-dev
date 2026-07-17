package validation

import (
	"context"

	"github.com/example/notification-service/internal/domain"
)

const (
	maxBodySize    = 64 * 1024 // 64 KiB
	maxSubjectSize = 256
)

type contentValidator struct {
	maxBody    int
	maxSubject int
}

// NewContentValidator returns a validator that checks the message payload.
func NewContentValidator() Validator {
	return &contentValidator{maxBody: maxBodySize, maxSubject: maxSubjectSize}
}

func (contentValidator) Name() string { return "content" }

func (c contentValidator) Validate(ctx context.Context, in Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := in.Notification.Message
	switch in.Notification.Channel {
	case domain.ChannelEmail:
		if m.Subject == "" {
			return domain.NewValidationError("missing_subject", "message.subject", "email requires a subject")
		}
		if len(m.Subject) > c.maxSubject {
			return domain.NewValidationError("subject_too_long", "message.subject", "subject exceeds maximum length")
		}
		if m.Body == "" {
			return domain.NewValidationError("empty_body", "message.body", "message body is empty")
		}
	case domain.ChannelSMS:
		if m.Body == "" {
			return domain.NewValidationError("empty_body", "message.body", "message body is empty")
		}
		if len(m.Body) > 1600 {
			return domain.NewValidationError("body_too_long", "message.body", "SMS body exceeds maximum length")
		}
	case domain.ChannelPush:
		if m.Body == "" && len(m.Data) == 0 {
			return domain.NewValidationError("empty_payload", "message", "push notification requires a body or data payload")
		}
	default:
		return domain.NewValidationError("unsupported_channel", "channel", "channel is not supported")
	}
	if len(m.Body) > c.maxBody {
		return domain.NewValidationError("body_too_long", "message.body", "body exceeds maximum length")
	}
	return nil
}
