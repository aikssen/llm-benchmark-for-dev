package validator

import (
	"context"
	"fmt"

	"github.com/assessment/notify-service/internal/domain"
)

type UserPreferences map[string]map[domain.Channel]bool

type PreferencesValidator struct {
	prefs UserPreferences
}

func NewPreferencesValidator() *PreferencesValidator {
	return &PreferencesValidator{
		prefs: UserPreferences{
			"blocked@example.com": {
				domain.ChannelEmail: false,
			},
		},
	}
}

func (v *PreferencesValidator) Name() string { return "preferences_validator" }

func (v *PreferencesValidator) Validate(_ context.Context, n *domain.Notification) (*domain.ValidationResult, error) {
	result := &domain.ValidationResult{Passed: true}

	for _, r := range n.Recipients {
		if prefs, ok := v.prefs[r.ID]; ok {
			if enabled, ok := prefs[n.Channel]; ok && !enabled {
				result.Passed = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("recipient %s has opted out of %s notifications", r.ID, n.Channel))
			}
		}
	}

	return result, nil
}
