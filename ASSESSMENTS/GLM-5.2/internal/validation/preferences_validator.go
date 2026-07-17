package validation

import (
	"context"

	"github.com/example/notification-service/internal/domain"
)

// PreferenceStore reports whether a recipient has opted in to a channel. This
// is a consumer-defined interface (Go idiom) so the validator does not depend
// on a concrete implementation.
type PreferenceStore interface {
	Allows(ctx context.Context, recipientID string, channel domain.Channel) (bool, error)
}

type preferenceValidator struct {
	store PreferenceStore
}

// NewPreferenceValidator returns a validator that checks the recipient's
// channel preferences.
func NewPreferenceValidator(store PreferenceStore) Validator {
	return &preferenceValidator{store: store}
}

func (preferenceValidator) Name() string { return "preferences" }

func (p preferenceValidator) Validate(ctx context.Context, in Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if in.Recipient.ID == "" {
		return domain.NewValidationError("missing_recipient_id", "recipient.id", "recipient id is required for preference checks")
	}
	allowed, err := p.store.Allows(ctx, in.Recipient.ID, in.Notification.Channel)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.NewValidationError("channel_opted_out", "preferences", "recipient has opted out of this channel")
	}
	return nil
}
