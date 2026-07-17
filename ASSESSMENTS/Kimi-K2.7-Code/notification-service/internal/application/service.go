package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/infrastructure/providers"
	"notification-service/pkg/workerpool"
)

// Service orchestrates notification submission, validation and delivery.
type Service struct {
	repo      domain.NotificationRepository
	registry  providers.Registry
	pool      *workerpool.Pool
	logger    *slog.Logger
	validators []Validator
}

// Validator validates a notification request before delivery.
type Validator interface {
	Name() string
	Validate(ctx context.Context, n domain.Notification) error
}

// NewService builds the application service.
func NewService(
	repo domain.NotificationRepository,
	registry providers.Registry,
	pool *workerpool.Pool,
	logger *slog.Logger,
) *Service {
	return &Service{
		repo:       repo,
		registry:   registry,
		pool:       pool,
		logger:     logger,
		validators: defaultValidators(),
	}
}

// SetValidators replaces the default validators. Useful for tests.
func (s *Service) SetValidators(v []Validator) {
	s.validators = v
}

// Start begins background processing.
func (s *Service) Start(ctx context.Context) {
	s.pool.Start(ctx, s.processJob)
}

// Shutdown waits for pending work to finish up to the given timeout.
func (s *Service) Shutdown(timeout time.Duration) {
	s.pool.Shutdown(timeout)
}

// processJob is invoked by the worker pool for each notification ID.
func (s *Service) processJob(ctx context.Context, id string) {
	s.logger.Info("processing notification", "id", id)

	n, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("failed to load notification", "id", id, "error", err)
		return
	}

	if err := s.validateAndDeliver(ctx, &n); err != nil {
		s.logger.Error("notification failed", "id", id, "error", err)
	}

	if saveErr := s.repo.Save(ctx, n); saveErr != nil {
		s.logger.Error("failed to persist notification", "id", id, "error", saveErr)
	}
}

// validateAndDeliver runs validations and, if successful, delivers the notification.
func (s *Service) validateAndDeliver(ctx context.Context, n *domain.Notification) error {
	n.SetStatus(domain.StatusValidating)

	if err := s.runValidations(ctx, *n); err != nil {
		n.MarkRejected(err.Error())
		return err
	}

	n.SetStatus(domain.StatusValidated)
	s.logger.Info("notification validated", "id", n.ID)

	n.SetStatus(domain.StatusSending)
	results, err := s.deliver(ctx, *n)
	if err != nil {
		n.MarkFailed(err.Error())
		return err
	}

	n.MarkSent(results)
	s.logger.Info("notification delivered", "id", n.ID)
	return nil
}

// runValidations executes all validators concurrently and returns early on the first failure.
func (s *Service) runValidations(ctx context.Context, n domain.Notification) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type response struct {
		name string
		err  error
	}

	resultCh := make(chan response, len(s.validators))
	for _, v := range s.validators {
		v := v
		s.pool.RunAsync(func() {
			// Respect cancellation so validators stop early when another fails.
			err := v.Validate(ctx, n)
			select {
			case resultCh <- response{name: v.Name(), err: err}:
			case <-ctx.Done():
			}
		})
	}

	for range s.validators {
		select {
		case res := <-resultCh:
			if res.err != nil {
				cancel() // stop remaining validators
				return fmt.Errorf("validation %q failed: %w", res.name, res.err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// deliver sends the notification to every recipient using the registered provider.
func (s *Service) deliver(ctx context.Context, n domain.Notification) ([]domain.RecipientResult, error) {
	provider, ok := s.registry.Get(n.Channel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrNoProvider, n.Channel)
	}

	results := make([]domain.RecipientResult, len(n.Recipients))
	for i, r := range n.Recipients {
		err := provider.Send(ctx, r, n.Content)
		res := domain.RecipientResult{
			Recipient: r,
			Success:   err == nil,
			Provider:  provider.Name(),
		}
		if err != nil {
			res.Error = err.Error()
		}
		results[i] = res
	}

	// Treat the whole delivery as failed if every recipient failed.
	allFailed := true
	for _, r := range results {
		if r.Success {
			allFailed = false
			break
		}
	}
	if allFailed {
		return results, domain.ErrProviderFailed
	}

	return results, nil
}
