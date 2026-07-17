package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/assessment/notify-service/internal/domain"
)

type RecipientValidator struct{}

func NewRecipientValidator() *RecipientValidator {
	return &RecipientValidator{}
}

func (v *RecipientValidator) Name() string { return "recipient_validator" }

func (v *RecipientValidator) Validate(_ context.Context, n *domain.Notification) (*domain.ValidationResult, error) {
	result := &domain.ValidationResult{Passed: true}

	for _, r := range n.Recipients {
		switch n.Channel {
		case domain.ChannelEmail:
			if !strings.Contains(r.ID, "@") {
				result.Passed = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("invalid email format for recipient %s", r.ID))
			}
		case domain.ChannelSMS:
			if len(r.ID) < 7 {
				result.Passed = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("invalid phone number for recipient %s", r.ID))
			}
		case domain.ChannelPush:
			if r.ID == "" {
				result.Passed = false
				result.Errors = append(result.Errors,
					"empty device token for push notification")
			}
		}
	}

	return result, nil
}
