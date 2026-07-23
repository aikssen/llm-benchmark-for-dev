package validator

import (
	"context"
	"sync"

	"notification-service/internal/domain"
)

type UserPreferenceValidator struct {
	mu            sync.RWMutex
	optOutSet map[string]bool
}

func NewUserPreferenceValidator(optedOutRecipients []string) *UserPreferenceValidator {
	set := make(map[string]bool)
	for _, r := range optedOutRecipients {
		set[r] = true
	}
	return &UserPreferenceValidator{
		optOutSet: set,
	}
}

func (upv *UserPreferenceValidator) Name() string {
	return "UserPreferenceValidator"
}

func (upv *UserPreferenceValidator) AddOptOut(recipient string) {
	upv.mu.Lock()
	defer upv.mu.Unlock()
	upv.optOutSet[recipient] = true
}

func (upv *UserPreferenceValidator) Validate(ctx context.Context, item *domain.SingleRecipientItem) error {
	upv.mu.RLock()
	defer upv.mu.RUnlock()

	if upv.optOutSet[item.Recipient] {
		return domain.ErrUserOptedOut
	}

	return nil
}
