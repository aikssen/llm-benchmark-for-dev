package validation

import (
	"context"
	"fmt"

	"github.com/aikssen/notification-service/internal/domain"
)

const maxBodyLength = 4096

// ContentValidator checks the message body is present and within bounds,
// and that channel-specific required fields (e.g. email subject) are set.
// It is intentionally cheap and allocation-free so it can run first and
// reject malformed batches before anything more expensive happens.
type ContentValidator struct{}

func (ContentValidator) Name() string { return "content" }

func (ContentValidator) Validate(_ context.Context, req domain.BatchRequest) error {
	if req.Message.Body == "" {
		return fmt.Errorf("%w: message body must not be empty", domain.ErrValidation)
	}
	if len(req.Message.Body) > maxBodyLength {
		return fmt.Errorf("%w: message body exceeds %d characters", domain.ErrValidation, maxBodyLength)
	}
	if req.Channel == domain.ChannelEmail && req.Message.Subject == "" {
		return fmt.Errorf("%w: subject is required for email notifications", domain.ErrValidation)
	}
	return nil
}
