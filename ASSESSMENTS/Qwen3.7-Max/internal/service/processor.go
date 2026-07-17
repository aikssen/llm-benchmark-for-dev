package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/notifications-service/internal/domain"
	"github.com/notifications-service/internal/provider"
	"github.com/notifications-service/internal/storage"
	"github.com/notifications-service/internal/validator"
)

type DeliveryJob struct {
	NotificationID string
	Channel        domain.Channel
	Subject        string
	Message        string
	Recipients     []string
}

type Processor struct {
	store      storage.Store
	registry   *provider.Registry
	validator  *validator.Chain
	logger     *slog.Logger
	jobQueue   chan DeliveryJob
	workers    int
	maxRetries int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewProcessor(
	store storage.Store,
	registry *provider.Registry,
	chain *validator.Chain,
	logger *slog.Logger,
	workers int,
	queueSize int,
) *Processor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Processor{
		store:      store,
		registry:   registry,
		validator:  chain,
		logger:     logger,
		jobQueue:   make(chan DeliveryJob, queueSize),
		workers:    workers,
		maxRetries: 3,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (p *Processor) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	p.logger.Info("processor started", "workers", p.workers)
}

func (p *Processor) Stop() {
	p.cancel()
	close(p.jobQueue)
	p.wg.Wait()
	p.logger.Info("processor stopped")
}

func (p *Processor) Enqueue(job DeliveryJob) {
	select {
	case p.jobQueue <- job:
	default:
		p.logger.Error("job queue is full, dropping notification",
			"notification_id", job.NotificationID,
		)
	}
}

func (p *Processor) worker(id int) {
	defer p.wg.Done()

	for job := range p.jobQueue {
		select {
		case <-p.ctx.Done():
			return
		default:
			p.processJob(job)
		}
	}
}

func (p *Processor) processJob(job DeliveryJob) {
	p.logger.Debug("processing notification",
		"notification_id", job.NotificationID,
		"recipients_count", len(job.Recipients),
	)

	provider, ok := p.registry.Get(job.Channel)
	if !ok {
		p.logger.Error("no provider for channel",
			"notification_id", job.NotificationID,
			"channel", job.Channel,
		)
		p.markNotificationFailed(job.NotificationID)
		return
	}

	var wg sync.WaitGroup
	results := make(chan deliveryResult, len(job.Recipients))

	for _, recipient := range job.Recipients {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			result := p.deliverToRecipient(provider, r, job)
			results <- result
		}(recipient)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var successCount, failCount int
	for result := range results {
		rs := &domain.RecipientNotification{
			NotificationID: job.NotificationID,
			Recipient:      result.Recipient,
			Channel:        job.Channel,
			Status:         result.Status,
			FailureReason:  result.FailureReason,
			DeliveredAt:    result.DeliveredAt,
		}
		if err := p.store.SaveRecipientStatus(p.ctx, rs); err != nil {
			p.logger.Error("failed to save recipient status",
				"notification_id", job.NotificationID,
				"recipient", result.Recipient,
				"error", err,
			)
		}

		if result.Status == domain.StatusDelivered {
			successCount++
		} else {
			failCount++
		}
	}

	finalStatus := domain.StatusDelivered
	if successCount == 0 && failCount > 0 {
		finalStatus = domain.StatusFailed
	}

	if err := p.store.UpdateStatus(p.ctx, job.NotificationID, finalStatus); err != nil {
		p.logger.Error("failed to update notification status",
			"notification_id", job.NotificationID,
			"error", err,
		)
	}

	p.logger.Info("notification processed",
		"notification_id", job.NotificationID,
		"status", finalStatus,
		"success_count", successCount,
		"fail_count", failCount,
	)
}

type deliveryResult struct {
	Recipient     string
	Status        domain.Status
	FailureReason string
	DeliveredAt   *time.Time
}

func (p *Processor) deliverToRecipient(provider provider.Provider, recipient string, job DeliveryJob) deliveryResult {
	req := &domain.NotificationRequest{
		Channel: job.Channel,
		Subject: job.Subject,
		Message: job.Message,
	}

	if err := p.validator.Validate(p.ctx, recipient, req); err != nil {
		p.logger.Warn("validation failed",
			"notification_id", job.NotificationID,
			"recipient", recipient,
			"error", err,
		)
		return deliveryResult{
			Recipient:     recipient,
			Status:        domain.StatusFailed,
			FailureReason: err.Error(),
		}
	}

	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-p.ctx.Done():
				return deliveryResult{
					Recipient:     recipient,
					Status:        domain.StatusFailed,
					FailureReason: "context cancelled",
				}
			}
		}

		ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
		err := provider.Send(ctx, recipient, job.Subject, job.Message)
		cancel()

		if err == nil {
			now := time.Now()
			return deliveryResult{
				Recipient:   recipient,
				Status:      domain.StatusDelivered,
				DeliveredAt: &now,
			}
		}

		lastErr = err
		if !isRetryable(err) {
			break
		}

		p.logger.Warn("delivery attempt failed, will retry",
			"notification_id", job.NotificationID,
			"recipient", recipient,
			"attempt", attempt+1,
			"error", err,
		)
	}

	return deliveryResult{
		Recipient:     recipient,
		Status:        domain.StatusFailed,
		FailureReason: lastErr.Error(),
	}
}

func (p *Processor) markNotificationFailed(id string) {
	if err := p.store.UpdateStatus(p.ctx, id, domain.StatusFailed); err != nil {
		p.logger.Error("failed to mark notification as failed",
			"notification_id", id,
			"error", err,
		)
	}
}

func isRetryable(err error) bool {
	var deliveryErr *domain.DeliveryError
	if errors.As(err, &deliveryErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}
