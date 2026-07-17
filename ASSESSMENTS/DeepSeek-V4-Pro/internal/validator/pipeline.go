package validator

import (
	"context"
	"sync"

	"github.com/assessment/notify-service/internal/domain"
)

type Pipeline struct {
	validators []domain.Validator
}

func NewPipeline(validators ...domain.Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

func (p *Pipeline) Validate(ctx context.Context, n *domain.Notification) *domain.ValidationResult {
	result := &domain.ValidationResult{Passed: true}

	var mu sync.Mutex
	var wg sync.WaitGroup
	failed := false

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, v := range p.validators {
		v := v
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			res, err := v.Validate(ctx, n)
			if err != nil {
				mu.Lock()
				if !failed {
					failed = true
					result.Passed = false
				}
				mu.Unlock()
				cancel()
				return
			}

			mu.Lock()
			if failed {
				mu.Unlock()
				return
			}

			if !res.Passed {
				failed = true
				result.Passed = false
				for _, e := range res.Errors {
					result.Errors = append(result.Errors, v.Name()+": "+e)
				}
				cancel()
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	return result
}
