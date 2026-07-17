package validator

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/assessment/notify-service/internal/domain"
)

const maxBodyLength = 10000

type ContentValidator struct{}

func NewContentValidator() *ContentValidator {
	return &ContentValidator{}
}

func (v *ContentValidator) Name() string { return "content_validator" }

func (v *ContentValidator) Validate(_ context.Context, n *domain.Notification) (*domain.ValidationResult, error) {
	result := &domain.ValidationResult{Passed: true}

	if utf8.RuneCountInString(n.Message.Body) > maxBodyLength {
		result.Passed = false
		result.Errors = append(result.Errors,
			fmt.Sprintf("message body exceeds maximum length of %d characters", maxBodyLength))
	}

	if n.Message.Subject != "" {
		switch n.Channel {
		case domain.ChannelSMS, domain.ChannelPush:
			result.Passed = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("subject is not supported for %s channel", n.Channel))
		}
	}

	return result, nil
}
