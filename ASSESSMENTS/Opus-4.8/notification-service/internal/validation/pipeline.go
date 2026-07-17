package validation

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/aikssen/notification-service/internal/domain"
)

// Pipeline runs a set of validators concurrently with fail-fast semantics.
//
// It uses errgroup, which derives a cancellable context and cancels it the
// moment any validator returns a non-nil error. Validators that observe the
// cancelled context return early, so the pipeline does not keep spending
// resources once the notification is already known to be invalid. The first
// error to occur is the one returned; later cancellation-induced errors are
// discarded by errgroup.
type Pipeline struct {
	validators []Validator
}

// NewPipeline builds a pipeline from the given validators. Order does not affect
// correctness because validators run concurrently; it only affects which error
// tends to win a race, and that is not relied upon.
func NewPipeline(validators ...Validator) *Pipeline {
	return &Pipeline{validators: validators}
}

// Validate runs every validator concurrently. It returns nil only if all pass.
// The returned error is wrapped with domain.ErrValidation so callers can branch
// on it with errors.Is.
func (p *Pipeline) Validate(ctx context.Context, n *domain.Notification) error {
	if len(p.validators) == 0 {
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, v := range p.validators {
		g.Go(func() error {
			if err := v.Validate(ctx, n); err != nil {
				// Preserve the validator name and wrap the sentinel so the API
				// layer can map this to a 4xx and still surface the reason.
				return fmt.Errorf("%w: %s: %v", domain.ErrValidation, v.Name(), err)
			}
			return nil
		})
	}
	return g.Wait()
}
