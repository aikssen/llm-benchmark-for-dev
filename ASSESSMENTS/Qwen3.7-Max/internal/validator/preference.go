package validator

import (
	"context"
	"sync"

	"github.com/notifications-service/internal/domain"
)

type PreferenceValidator struct {
	mu      sync.RWMutex
	optedOut map[string]map[domain.Channel]bool
}

func NewPreferenceValidator() *PreferenceValidator {
	return &PreferenceValidator{
		optedOut: make(map[string]map[domain.Channel]bool),
	}
}

func (v *PreferenceValidator) Name() string {
	return "preference"
}

func (v *PreferenceValidator) Validate(_ context.Context, recipient string, req *domain.NotificationRequest) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if channels, ok := v.optedOut[recipient]; ok {
		if channels[req.Channel] {
			return domain.ErrUserOptedOut
		}
	}

	return nil
}

func (v *PreferenceValidator) SetOptOut(recipient string, channel domain.Channel, optedOut bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.optedOut[recipient]; !ok {
		v.optedOut[recipient] = make(map[domain.Channel]bool)
	}
	v.optedOut[recipient][channel] = optedOut
}
