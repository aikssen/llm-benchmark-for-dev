// Package validation runs the pre-delivery checks a notification must pass.
//
// Validators are independent and side-effect free with respect to each other,
// which lets the pipeline run them concurrently. As soon as one fails, the
// shared context is cancelled so the remaining validators stop early instead of
// consuming resources on work whose result can no longer change the outcome.
package validation

import (
	"context"

	"github.com/aikssen/notification-service/internal/domain"
)

// Validator performs a single independent check against a notification. A nil
// error means the check passed. Implementations must respect ctx cancellation
// and return promptly when it fires, since a sibling validator failing cancels
// the whole pipeline.
type Validator interface {
	// Name identifies the validator in errors and logs.
	Name() string
	// Validate returns nil on success or a non-nil error describing why the
	// notification is rejected.
	Validate(ctx context.Context, n *domain.Notification) error
}

// ValidatorFunc adapts a plain function to the Validator interface for simple,
// stateless checks.
type ValidatorFunc struct {
	ValidatorName string
	Fn            func(ctx context.Context, n *domain.Notification) error
}

func (f ValidatorFunc) Name() string { return f.ValidatorName }

func (f ValidatorFunc) Validate(ctx context.Context, n *domain.Notification) error {
	return f.Fn(ctx, n)
}
