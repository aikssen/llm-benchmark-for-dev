package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/validation"
)

// Queue defines the interface for submitting jobs for background execution
type Queue interface {
	Enqueue(ctx context.Context, id string) error
}

// Orchestrator coordinates notification creation, validation, scheduling, and delivery
type Orchestrator struct {
	repo             repository.Repository
	validationEngine *validation.Engine
	providerRegistry *provider.Registry
	queue            Queue
}

// SubmitRequest defines the payload for submitting a notification request
type SubmitRequest struct {
	Recipients []string       `json:"recipients"`
	Channel    domain.Channel `json:"channel"`
	Content    string         `json:"content"`
}

// SubmitResponse defines the response returned after submitting a request
type SubmitResponse struct {
	RequestID       string   `json:"request_id"`
	NotificationIDs []string `json:"notification_ids"`
}

// NewOrchestrator creates a new Orchestrator instance
func NewOrchestrator(
	repo repository.Repository,
	validationEngine *validation.Engine,
	providerRegistry *provider.Registry,
) *Orchestrator {
	return &Orchestrator{
		repo:             repo,
		validationEngine: validationEngine,
		providerRegistry: providerRegistry,
	}
}

// SetQueue configures the queue for the orchestrator (breaks circular dependency during startup)
func (s *Orchestrator) SetQueue(q Queue) {
	s.queue = q
}

// Submit handles the incoming request by validating arguments, persisting notification records,
// and queuing them for background validation and delivery.
func (s *Orchestrator) Submit(ctx context.Context, req *SubmitRequest) (*SubmitResponse, error) {
	if len(req.Recipients) == 0 {
		return nil, errors.New("at least one recipient is required")
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, errors.New("notification content cannot be empty")
	}
	if err := req.Channel.Validate(); err != nil {
		return nil, err
	}

	requestID, err := generateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate request ID: %w", err)
	}

	notificationIDs := make([]string, 0, len(req.Recipients))

	for _, recipient := range req.Recipients {
		notificationID, err := generateUUID()
		if err != nil {
			return nil, fmt.Errorf("failed to generate notification ID: %w", err)
		}

		n := &domain.Notification{
			ID:         notificationID,
			RequestID:  requestID,
			Recipient:  recipient,
			Channel:    req.Channel,
			Content:    req.Content,
			Status:     domain.StatusPending,
			RetryCount: 0,
		}

		if err := s.repo.Save(ctx, n); err != nil {
			return nil, fmt.Errorf("failed to save notification: %w", err)
		}

		notificationIDs = append(notificationIDs, n.ID)

		// Push to background queue
		if s.queue != nil {
			if err := s.queue.Enqueue(ctx, n.ID); err != nil {
				// We log or track if queue is full, but we continue submitting others if possible
				// Or fail request. Since this is critical, we return an error if enqueueing fails.
				return nil, fmt.Errorf("failed to queue notification %s: %w", n.ID, err)
			}
		}
	}

	return &SubmitResponse{
		RequestID:       requestID,
		NotificationIDs: notificationIDs,
	}, nil
}

// ProcessNotification executes the lifecycle of a notification:
// 1. Mark as Validating -> Run Validation Engine.
// 2. Mark as Sending -> Deliver through primary and fallback providers with exponential backoff.
// 3. Mark as Sent or Failed depending on outcome.
func (s *Orchestrator) ProcessNotification(ctx context.Context, id string) error {
	n, err := s.repo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to retrieve notification from repo: %w", err)
	}

	// Guard to ensure we don't double process
	if n.Status != domain.StatusPending {
		return nil
	}

	// 1. Move to Validating status
	n.Status = domain.StatusValidating
	_ = s.repo.Update(ctx, n)

	// 2. Run validations
	if err := s.validationEngine.Validate(ctx, n); err != nil {
		n.Status = domain.StatusFailedValidation
		n.ErrorReason = err.Error()
		_ = s.repo.Update(ctx, n)
		return fmt.Errorf("validation failed: %w", err)
	}

	// 3. Move to Sending status
	n.Status = domain.StatusSending
	_ = s.repo.Update(ctx, n)

	// 4. Deliver
	if err := s.deliverWithRetry(ctx, n); err != nil {
		n.Status = domain.StatusFailed
		n.ErrorReason = err.Error()
		_ = s.repo.Update(ctx, n)
		return fmt.Errorf("delivery failed: %w", err)
	}

	// 5. Success!
	n.Status = domain.StatusSent
	_ = s.repo.Update(ctx, n)
	return nil
}

func (s *Orchestrator) deliverWithRetry(ctx context.Context, n *domain.Notification) error {
	providers, err := s.providerRegistry.GetProviders(n.Channel)
	if err != nil {
		return err
	}

	var lastErr error
	for _, prov := range providers {
		backoff := 50 * time.Millisecond
		maxAttempts := 3

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			// Update retry count (attempt starts at 1, but RetryCount = attempt-1 represents retries)
			n.RetryCount = attempt - 1
			_ = s.repo.Update(ctx, n)

			err = prov.Send(ctx, n)
			if err == nil {
				return nil // Sent successfully
			}

			lastErr = fmt.Errorf("provider %s failed (attempt %d/%d): %w", prov.Name(), attempt, maxAttempts, err)

			if attempt < maxAttempts {
				select {
				case <-time.After(backoff):
					backoff *= 2
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return lastErr
}

func generateUUID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
