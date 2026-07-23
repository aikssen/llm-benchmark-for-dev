package validator

import (
	"context"
	"fmt"
	"sync"

	"notification-service/internal/domain"
)

// Validator interface for extensible validation rules.
type Validator interface {
	Name() string
	Validate(ctx context.Context, item *domain.SingleRecipientItem) error
}

// Pipeline orchestrates concurrent validation execution with fast-failing context cancellation.
type Pipeline struct {
	validators []Validator
}

func NewPipeline(validators ...Validator) *Pipeline {
	return &Pipeline{
		validators: validators,
	}
}

// Validate executes all validators concurrently.
// If any validator fails, the remaining validations are cancelled immediately to save resources.
func (p *Pipeline) Validate(ctx context.Context, item *domain.SingleRecipientItem) error {
	if len(p.validators) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, len(p.validators))
	var wg sync.WaitGroup

	for _, v := range p.validators {
		wg.Add(1)
		go func(val Validator) {
			defer wg.Done()

			// Check context before doing work
			select {
			case <-ctx.Done():
				return
			default:
			}

			err := val.Validate(ctx, item)
			if err != nil {
				select {
				case errChan <- fmt.Errorf("%s validation failed: %w", val.Name(), err):
					cancel() // Cancel context to stop all other concurrent validators
				case <-ctx.Done():
				}
			}
		}(v)
	}

	wg.Wait()
	close(errChan)

	// If there's any error in the channel, return the first one
	if err, ok := <-errChan; ok {
		return err
	}

	return nil
}
