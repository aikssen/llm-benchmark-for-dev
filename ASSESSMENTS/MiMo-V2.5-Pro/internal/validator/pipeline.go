package validator

import (
	"context"
	"log/slog"
	"sync"

	"github.com/notification-service/internal/domain"
)

// Pipeline executes multiple validators concurrently.
// If any validation fails, the pipeline short-circuits and returns immediately.
type Pipeline struct {
	validators []domain.Validator
	logger     *slog.Logger
}

// NewPipeline creates a new validation pipeline.
func NewPipeline(logger *slog.Logger, validators ...domain.Validator) *Pipeline {
	return &Pipeline{
		validators: validators,
		logger:     logger,
	}
}

// Validate runs all validators concurrently and returns on the first error.
func (p *Pipeline) Validate(ctx context.Context, req *domain.CreateNotificationRequest) error {
	if len(p.validators) == 0 {
		return nil
	}

	errChan := make(chan error, len(p.validators))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	for _, v := range p.validators {
		wg.Add(1)
		go func(validator domain.Validator) {
			defer wg.Done()
			if err := validator.Validate(ctx, req); err != nil {
				p.logger.Debug("validation failed",
					"validator", validator.Name(),
					"error", err.Error(),
				)
				select {
				case errChan <- err:
					cancel() // Signal other validators to stop
				case <-ctx.Done():
				}
			}
		}(v)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
