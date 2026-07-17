package validation

import (
	"context"
	"errors"
	"fmt"
	"notification-service/internal/domain"
)

// Engine orchestrates running multiple independent validators concurrently
type Engine struct {
	validators []Validator
}

// NewEngine creates a new Engine with the given validators
func NewEngine(validators ...Validator) *Engine {
	return &Engine{validators: validators}
}

// Validate executes all validators concurrently. If one validator fails, the remaining
// validations are immediately canceled via context propagation to save resources.
func (e *Engine) Validate(ctx context.Context, n *domain.Notification) error {
	if len(e.validators) == 0 {
		return nil
	}

	// Create a cancelable context linked to the parent context.
	// If any validation fails, we will call cancel to abort others.
	vCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, len(e.validators))

	for _, v := range e.validators {
		go func(val Validator) {
			err := val.Validate(vCtx, n)
			if err != nil {
				// Cancel other goroutines since this one failed
				cancel()

				// If this goroutine was cancelled internally (meaning another validator failed first)
				// and the external parent context is NOT cancelled, we treat this as a silent abort.
				if errors.Is(err, context.Canceled) && ctx.Err() == nil {
					errChan <- nil
				} else {
					errChan <- fmt.Errorf("[%s]: %w", val.Name(), err)
				}
			} else {
				errChan <- nil
			}
		}(v)
	}

	var firstErr error
	for i := 0; i < len(e.validators); i++ {
		err := <-errChan
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
