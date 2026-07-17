package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/notification-service/internal/domain"
	"github.com/notification-service/internal/provider"
	"github.com/notification-service/internal/validator"
)

// NotificationService handles notification business logic.
type NotificationService struct {
	repo      domain.Repository
	providers *provider.Registry
	pipeline  *validator.Pipeline
	logger    *slog.Logger
	queue     chan *domain.Notification
	wg        sync.WaitGroup
	workers   int
}

// NewNotificationService creates a new notification service.
func NewNotificationService(
	repo domain.Repository,
	providers *provider.Registry,
	pipeline *validator.Pipeline,
	logger *slog.Logger,
	workers int,
	queueSize int,
) *NotificationService {
	return &NotificationService{
		repo:      repo,
		providers: providers,
		pipeline:  pipeline,
		logger:    logger,
		queue:     make(chan *domain.Notification, queueSize),
		workers:   workers,
	}
}

// Start begins the worker pool for processing notifications.
func (s *NotificationService) Start(ctx context.Context) {
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx, i)
	}
	s.logger.Info("notification service started", "workers", s.workers)
}

// Stop gracefully shuts down the service.
func (s *NotificationService) Stop() {
	close(s.queue)
	s.wg.Wait()
	s.logger.Info("notification service stopped")
}

// CreateNotification creates and queues a new notification.
func (s *NotificationService) CreateNotification(ctx context.Context, req *domain.CreateNotificationRequest) (*domain.Notification, error) {
	if err := s.pipeline.Validate(ctx, req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	notification := &domain.Notification{
		ID:         uuid.New().String(),
		Recipients: req.Recipients,
		Channel:    req.Channel,
		Content:    req.Content,
		Status:     domain.StatusPending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Metadata:   req.Metadata,
		Results:    make([]domain.Result, len(req.Recipients)),
	}

	for i, r := range req.Recipients {
		notification.Results[i] = domain.Result{
			Recipient: r,
			Status:    domain.StatusPending,
		}
	}

	if err := s.repo.Save(ctx, notification); err != nil {
		return nil, fmt.Errorf("failed to save notification: %w", err)
	}

	s.logger.Info("notification created",
		"id", notification.ID,
		"channel", notification.Channel,
		"recipients", len(notification.Recipients),
	)

	select {
	case s.queue <- notification:
		notification.Status = domain.StatusQueued
		notification.UpdatedAt = time.Now().UTC()
		if err := s.repo.Update(ctx, notification); err != nil {
			s.logger.Error("failed to update notification status", "error", err)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return notification, nil
}

// GetNotification retrieves a notification by ID.
func (s *NotificationService) GetNotification(ctx context.Context, id string) (*domain.Notification, error) {
	return s.repo.GetByID(ctx, id)
}

// ListNotifications retrieves notifications based on parameters.
func (s *NotificationService) ListNotifications(ctx context.Context, params domain.ListNotificationsParams) ([]*domain.Notification, int, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	return s.repo.List(ctx, params)
}

// worker processes notifications from the queue.
func (s *NotificationService) worker(ctx context.Context, id int) {
	defer s.wg.Done()

	s.logger.Info("worker started", "worker_id", id)

	for notification := range s.queue {
		s.processNotification(ctx, notification)
	}

	s.logger.Info("worker stopped", "worker_id", id)
}

// processNotification handles the delivery of a notification.
func (s *NotificationService) processNotification(ctx context.Context, notification *domain.Notification) {
	notification.Status = domain.StatusSending
	notification.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, notification); err != nil {
		s.logger.Error("failed to update notification status",
			"id", notification.ID,
			"error", err,
		)
		return
	}

	provider, err := s.providers.GetProvider(notification.Channel)
	if err != nil {
		s.markAllFailed(notification, err.Error())
		s.repo.Update(ctx, notification)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	allSent := true
	anySent := false

	for i, recipient := range notification.Recipients {
		wg.Add(1)
		go func(idx int, r domain.Recipient) {
			defer wg.Done()

			sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			err := s.sendWithRetry(sendCtx, provider, r, notification.Content, 3)

			mu.Lock()
			defer mu.Unlock()

			now := time.Now().UTC()
			if err != nil {
				notification.Results[idx].Status = domain.StatusFailed
				notification.Results[idx].Error = err.Error()
				allSent = false
			} else {
				notification.Results[idx].Status = domain.StatusSent
				notification.Results[idx].SentAt = &now
				anySent = true
			}
		}(i, recipient)
	}

	wg.Wait()

	if allSent {
		notification.Status = domain.StatusSent
	} else if anySent {
		notification.Status = domain.StatusPartial
	} else {
		notification.Status = domain.StatusFailed
	}

	notification.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, notification); err != nil {
		s.logger.Error("failed to update notification final status",
			"id", notification.ID,
			"error", err,
		)
	}

	s.logger.Info("notification processed",
		"id", notification.ID,
		"status", notification.Status,
	)
}

// sendWithRetry attempts to send a notification with exponential backoff.
func (s *NotificationService) sendWithRetry(
	ctx context.Context,
	p domain.Provider,
	recipient domain.Recipient,
	content domain.Content,
	maxRetries int,
) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}

			s.logger.Debug("retrying send",
				"provider", p.Name(),
				"recipient", recipient.Value,
				"attempt", attempt+1,
			)
		}

		if err := p.Send(ctx, recipient, content); err != nil {
			lastErr = err
			s.logger.Warn("send attempt failed",
				"provider", p.Name(),
				"recipient", recipient.Value,
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}

		return nil
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (s *NotificationService) markAllFailed(notification *domain.Notification, errMsg string) {
	notification.Status = domain.StatusFailed
	for i := range notification.Results {
		notification.Results[i].Status = domain.StatusFailed
		notification.Results[i].Error = errMsg
	}
	notification.UpdatedAt = time.Now().UTC()
}
