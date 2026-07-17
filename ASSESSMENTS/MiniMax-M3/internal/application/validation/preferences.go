package validation

import (
	"context"

	"github.com/anomalyco/notification-service/internal/domain"
)

type PreferenceStore interface {
	IsOptedOut(ctx context.Context, userID string, channel domain.Channel) (bool, error)
}

type PreferenceValidator struct {
	store PreferenceStore
}

func NewPreferenceValidator(s PreferenceStore) *PreferenceValidator {
	return &PreferenceValidator{store: s}
}

func (PreferenceValidator) Name() string { return "preferences" }

func (v *PreferenceValidator) Validate(ctx context.Context, n *domain.Notification) error {
	optedOut, err := v.store.IsOptedOut(ctx, n.Recipient, n.Channel)
	if err != nil {
		return err
	}
	if optedOut {
		return domain.ErrUserOptedOut
	}
	return nil
}
