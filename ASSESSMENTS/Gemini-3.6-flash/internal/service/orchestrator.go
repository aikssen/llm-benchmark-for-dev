package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/validator"
	"notification-service/internal/worker"
)

type Orchestrator interface {
	SubmitNotification(ctx context.Context, input SubmitNotificationInput) (*domain.NotificationRequest, error)
	GetNotificationStatus(ctx context.Context, id string) (*domain.NotificationRequest, error)
	ListNotifications(ctx context.Context, filter domain.NotificationFilter) (*domain.ListResult, error)
}

type SubmitNotificationInput struct {
	Channel    domain.Channel    `json:"channel"`
	Recipients []string          `json:"recipients"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Service struct {
	repo       repository.Repository
	pipeline   *validator.Pipeline
	registry   *provider.Registry
	workerPool *worker.Pool
}

func NewService(repo repository.Repository, pipeline *validator.Pipeline, registry *provider.Registry, poolWorkers, poolCapacity int) *Service {
	s := &Service{
		repo:     repo,
		pipeline: pipeline,
		registry: registry,
	}

	// Initialize worker pool passing s.processItem as the handler
	s.workerPool = worker.NewPool(poolWorkers, poolCapacity, s.processItem)
	return s
}

func (s *Service) SubmitNotification(ctx context.Context, input SubmitNotificationInput) (*domain.NotificationRequest, error) {
	// Basic input validation
	channel := input.Channel.Normalized()
	if !channel.IsValid() {
		return nil, domain.ErrInvalidChannel
	}
	if len(input.Recipients) == 0 {
		return nil, domain.ErrEmptyRecipients
	}
	if strings.TrimSpace(input.Body) == "" {
		return nil, domain.ErrEmptyContent
	}

	now := time.Now()
	requestID := uuid.New().String()

	req := &domain.NotificationRequest{
		ID:              requestID,
		Channel:         channel,
		Recipients:      input.Recipients,
		Subject:         input.Subject,
		Body:            input.Body,
		Metadata:        input.Metadata,
		Status:          domain.StatusQueued,
		TotalRecipients: len(input.Recipients),
		DeliveredCount:  0,
		FailedCount:     0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.SaveRequest(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to save request: %w", err)
	}

	// Create recipient details and submit to worker pool
	var items []*domain.SingleRecipientItem
	for _, recipient := range input.Recipients {
		recStatusID := uuid.New().String()
		recStatus := &domain.NotificationRecipientStatus{
			ID:        recStatusID,
			RequestID: requestID,
			Recipient: recipient,
			Channel:   channel,
			Status:    domain.StatusQueued,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.repo.SaveRecipientStatus(ctx, recStatus); err != nil {
			slog.Error("Failed to save recipient status", "request_id", requestID, "recipient", recipient, "error", err)
		}

		items = append(items, &domain.SingleRecipientItem{
			ID:        recStatusID,
			RequestID: requestID,
			Channel:   channel,
			Recipient: recipient,
			Subject:   input.Subject,
			Body:      input.Body,
			Metadata:  input.Metadata,
			CreatedAt: now,
		})
	}

	// Async enqueue to worker pool
	for _, item := range items {
		if err := s.workerPool.Submit(item); err != nil {
			slog.Warn("Queue full when submitting item", "item_id", item.ID, "error", err)
			_ = s.repo.UpdateRecipientStatus(ctx, item.ID, domain.StatusFailed, "", err.Error(), 0)
			_ = s.repo.UpdateAggregateStatus(ctx, requestID)
		}
	}

	return s.repo.GetRequestByID(ctx, requestID)
}

func (s *Service) processItem(ctx context.Context, item *domain.SingleRecipientItem) error {
	// 1. Mark status as validating
	_ = s.repo.UpdateRecipientStatus(ctx, item.ID, domain.StatusValidating, "", "", 1)

	// 2. Execute concurrent fast-failing validation pipeline
	if err := s.pipeline.Validate(ctx, item); err != nil {
		slog.Info("Validation failed for notification item", "item_id", item.ID, "recipient", item.Recipient, "error", err)
		_ = s.repo.UpdateRecipientStatus(ctx, item.ID, domain.StatusInvalid, "", err.Error(), 1)
		_ = s.repo.UpdateAggregateStatus(ctx, item.RequestID)
		return nil
	}

	// 3. Mark status as processing
	_ = s.repo.UpdateRecipientStatus(ctx, item.ID, domain.StatusProcessing, "", "", 1)

	// 4. Send via provider registry (includes retries & circuit breakers)
	providerUsed, err := s.registry.Send(ctx, item)
	if err != nil {
		slog.Error("Delivery failed for notification item", "item_id", item.ID, "recipient", item.Recipient, "error", err)
		_ = s.repo.UpdateRecipientStatus(ctx, item.ID, domain.StatusFailed, providerUsed, err.Error(), 1)
		_ = s.repo.UpdateAggregateStatus(ctx, item.RequestID)
		return nil
	}

	// 5. Success
	_ = s.repo.UpdateRecipientStatus(ctx, item.ID, domain.StatusDelivered, providerUsed, "", 1)
	_ = s.repo.UpdateAggregateStatus(ctx, item.RequestID)
	return nil
}

func (s *Service) GetNotificationStatus(ctx context.Context, id string) (*domain.NotificationRequest, error) {
	return s.repo.GetRequestByID(ctx, id)
}

func (s *Service) ListNotifications(ctx context.Context, filter domain.NotificationFilter) (*domain.ListResult, error) {
	return s.repo.ListRequests(ctx, filter)
}

func (s *Service) Stop() {
	if s.workerPool != nil {
		s.workerPool.Stop()
	}
}
