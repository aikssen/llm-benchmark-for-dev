package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"notification/internal/notification"
)

type Job struct {
	NotificationID string
	DeliveryIndex  int
}

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Timeout     time.Duration
}

type Processor struct {
	repository notification.Repository
	providers  notification.ProviderRegistry
	validators []notification.Validator
	retry      RetryPolicy
	now        func() time.Time
}

func NewProcessor(repository notification.Repository, providers notification.ProviderRegistry, validators []notification.Validator, retry RetryPolicy) *Processor {
	return &Processor{
		repository: repository,
		providers:  providers,
		validators: append([]notification.Validator(nil), validators...),
		retry:      retry,
		now:        time.Now,
	}
}

func (p *Processor) Process(ctx context.Context, job Job) {
	value, err := p.repository.Get(ctx, job.NotificationID)
	if err != nil || job.DeliveryIndex < 0 || job.DeliveryIndex >= len(value.Deliveries) {
		return
	}
	delivery := value.Deliveries[job.DeliveryIndex]
	if delivery.Status.Terminal() {
		return
	}

	delivery.Status = notification.DeliveryProcessing
	delivery.UpdatedAt = p.now().UTC()
	if _, err = p.repository.SaveDelivery(ctx, value.ID, job.DeliveryIndex, delivery); err != nil {
		return
	}

	for _, validator := range p.validators {
		if err = validator.Validate(ctx, value.Channel, delivery.Recipient, value.Message); err != nil {
			delivery.Status = notification.DeliveryRejected
			delivery.ErrorCode = errorCode(err, "validation_failed")
			delivery.Error = fmt.Sprintf("%s: %v", validator.Name(), err)
			delivery.UpdatedAt = p.now().UTC()
			_, _ = p.repository.SaveDelivery(context.WithoutCancel(ctx), value.ID, job.DeliveryIndex, delivery)
			return
		}
	}

	provider, ok := p.providers.Provider(value.Channel)
	if !ok {
		p.fail(ctx, value.ID, job.DeliveryIndex, delivery, "provider_unavailable", "no provider configured for channel")
		return
	}

	for attempt := 1; attempt <= p.retry.MaxAttempts; attempt++ {
		delivery.Attempts = attempt
		delivery.Status = notification.DeliveryProcessing
		if attempt > 1 {
			delivery.Status = notification.DeliveryRetrying
		}
		delivery.UpdatedAt = p.now().UTC()
		if _, err = p.repository.SaveDelivery(ctx, value.ID, job.DeliveryIndex, delivery); err != nil {
			return
		}

		attemptCtx, cancel := context.WithTimeout(ctx, p.retry.Timeout)
		result, sendErr := provider.Send(attemptCtx, notification.ProviderMessage{
			NotificationID: value.ID,
			Recipient:      delivery.Recipient,
			Message:        value.Message,
		})
		cancel()

		if sendErr == nil {
			delivery.Status = notification.DeliveryDelivered
			delivery.ProviderID = result.ID
			delivery.ErrorCode = ""
			delivery.Error = ""
			delivery.UpdatedAt = p.now().UTC()
			_, _ = p.repository.SaveDelivery(context.WithoutCancel(ctx), value.ID, job.DeliveryIndex, delivery)
			return
		}

		if ctx.Err() != nil {
			p.fail(ctx, value.ID, job.DeliveryIndex, delivery, "processing_cancelled", "processing was cancelled")
			return
		}
		if attempt == p.retry.MaxAttempts || !isRetryable(sendErr) {
			p.fail(ctx, value.ID, job.DeliveryIndex, delivery, "delivery_failed", sendErr.Error())
			return
		}

		delivery.Status = notification.DeliveryRetrying
		delivery.ErrorCode = "provider_retryable_error"
		delivery.Error = sendErr.Error()
		delivery.UpdatedAt = p.now().UTC()
		_, _ = p.repository.SaveDelivery(ctx, value.ID, job.DeliveryIndex, delivery)
		if !wait(ctx, p.backoff(value.ID, job.DeliveryIndex, attempt)) {
			p.fail(ctx, value.ID, job.DeliveryIndex, delivery, "processing_cancelled", "processing was cancelled")
			return
		}
	}
}

func (p *Processor) fail(ctx context.Context, id string, index int, delivery notification.Delivery, code, message string) {
	delivery.Status = notification.DeliveryFailed
	delivery.ErrorCode = code
	delivery.Error = message
	delivery.UpdatedAt = p.now().UTC()
	_, _ = p.repository.SaveDelivery(context.WithoutCancel(ctx), id, index, delivery)
}

func (p *Processor) backoff(id string, index, attempt int) time.Duration {
	delay := p.retry.BaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= p.retry.MaxDelay/2 {
			delay = p.retry.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > p.retry.MaxDelay {
		delay = p.retry.MaxDelay
	}
	// Deterministic 50-100% jitter distributes retries and keeps tests reproducible.
	var hash uint64 = 1469598103934665603
	for _, char := range fmt.Sprintf("%s:%d:%d", id, index, attempt) {
		hash ^= uint64(char)
		hash *= 1099511628211
	}
	return delay/2 + time.Duration(hash%uint64(max(delay/2, 1)))
}

func isRetryable(err error) bool {
	var retryable notification.RetryableError
	return errors.As(err, &retryable) && retryable.Retryable()
}

func errorCode(err error, fallback string) string {
	var coded notification.CodedError
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return fallback
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
