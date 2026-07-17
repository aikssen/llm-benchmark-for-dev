// Package validation implements the pre-delivery validation pipeline.
//
// Validations are split into two tiers, run in this order:
//
//  1. Request-level validators run once per submitted batch, against
//     data shared by every recipient (currently: message content). If
//     one fails, the whole batch is rejected before a single
//     Notification row is created or any recipient is looked at — the
//     cheapest possible fail-fast, per the requirement that a failed
//     validation must not cause unnecessary downstream work.
//
//  2. Recipient-level validators run once per recipient, in a fixed
//     order from cheapest/local to most expensive: address format,
//     then user preferences, then rate limiting. The chain stops at the
//     first failure for that recipient — but a failure only affects
//     that recipient's Notification, not its siblings in the batch,
//     since each is otherwise independent.
//
// Both tiers are ordinary interfaces, so adding a new validation (e.g.
// blocklist checking, content moderation) is a matter of implementing the
// interface and appending it to the pipeline slice built in main.go —
// nothing here needs to change.
package validation

import (
	"context"

	"github.com/aikssen/notification-service/internal/domain"
)

// RequestValidator validates data shared across an entire batch request.
type RequestValidator interface {
	// Name identifies the validator for logging/diagnostics.
	Name() string
	Validate(ctx context.Context, req domain.BatchRequest) error
}

// RecipientValidator validates a single recipient within a batch.
type RecipientValidator interface {
	Name() string
	Validate(ctx context.Context, channel domain.Channel, recipient string) error
}

// Pipeline runs request- and recipient-level validators in order,
// short-circuiting on the first failure at each tier.
type Pipeline struct {
	requestValidators   []RequestValidator
	recipientValidators []RecipientValidator
}

// NewPipeline builds a Pipeline. Order matters: validators run in the
// order given, and execution stops at the first error.
func NewPipeline(requestValidators []RequestValidator, recipientValidators []RecipientValidator) *Pipeline {
	return &Pipeline{
		requestValidators:   requestValidators,
		recipientValidators: recipientValidators,
	}
}

// ValidateRequest runs all request-level validators, stopping at the first error.
func (p *Pipeline) ValidateRequest(ctx context.Context, req domain.BatchRequest) error {
	for _, v := range p.requestValidators {
		if err := v.Validate(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRecipient runs all recipient-level validators for one recipient,
// stopping at the first error.
func (p *Pipeline) ValidateRecipient(ctx context.Context, channel domain.Channel, recipient string) error {
	for _, v := range p.recipientValidators {
		if err := v.Validate(ctx, channel, recipient); err != nil {
			return err
		}
	}
	return nil
}
