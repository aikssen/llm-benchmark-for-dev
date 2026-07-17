package validator

import (
	"context"

	"github.com/notifications-service/internal/domain"
)

type Validator interface {
	Validate(ctx context.Context, recipient string, req *domain.NotificationRequest) error
	Name() string
}

type Chain struct {
	validators []Validator
}

func NewChain(validators ...Validator) *Chain {
	return &Chain{validators: validators}
}

func (c *Chain) Validate(ctx context.Context, recipient string, req *domain.NotificationRequest) error {
	for _, v := range c.validators {
		if err := v.Validate(ctx, recipient, req); err != nil {
			return err
		}
	}
	return nil
}
