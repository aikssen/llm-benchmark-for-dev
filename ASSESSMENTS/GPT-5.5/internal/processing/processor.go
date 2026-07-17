package processing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/store"
	"notification-service/internal/validation"
)

var (
	ErrQueueFull = errors.New("notification queue is full")
	ErrStopped   = errors.New("processor is stopped")
)

type NotificationStore interface {
	Get(ctx context.Context, id string) (domain.Notification, error)
	Update(ctx context.Context, id string, update func(*domain.Notification)) (domain.Notification, error)
}

type Config struct {
	Workers      int
	QueueSize    int
	MaxAttempts  int
	SendTimeout  time.Duration
	RetryBackoff time.Duration
}

type Processor struct {
	store      NotificationStore
	providers  *provider.Registry
	validators []validation.Validator
	config     Config

	jobs      chan string
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	enqueueMu sync.Mutex
	startOnce sync.Once
	stopOnce  sync.Once
	mu        sync.RWMutex
	isRunning bool
}

func NewProcessor(
	store NotificationStore,
	providers *provider.Registry,
	validators []validation.Validator,
	cfg Config,
) *Processor {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 100
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = 2 * time.Second
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 100 * time.Millisecond
	}

	return &Processor{
		store:      store,
		providers:  providers,
		validators: append([]validation.Validator(nil), validators...),
		config:     cfg,
		jobs:       make(chan string, cfg.QueueSize),
	}
}

func (p *Processor) Start(parent context.Context) {
	p.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		p.cancel = cancel

		p.mu.Lock()
		p.isRunning = true
		p.mu.Unlock()

		for workerID := 0; workerID < p.config.Workers; workerID++ {
			p.wg.Add(1)
			go p.worker(ctx)
		}
	})
}

func (p *Processor) Stop() {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.isRunning = false
		p.mu.Unlock()

		if p.cancel != nil {
			p.cancel()
		}
		p.wg.Wait()
	})
}

func (p *Processor) Enqueue(ctx context.Context, notificationID string) error {
	return p.EnqueueBatch(ctx, []string{notificationID})
}

func (p *Processor) EnqueueBatch(ctx context.Context, notificationIDs []string) error {
	if !p.running() {
		return ErrStopped
	}
	if len(notificationIDs) == 0 {
		return nil
	}
	if len(notificationIDs) > cap(p.jobs) {
		return ErrQueueFull
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	p.enqueueMu.Lock()
	defer p.enqueueMu.Unlock()

	if len(p.jobs)+len(notificationIDs) > cap(p.jobs) {
		return ErrQueueFull
	}

	for _, notificationID := range notificationIDs {
		p.jobs <- notificationID
	}
	return nil
}

func (p *Processor) running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isRunning
}

func (p *Processor) worker(ctx context.Context) {
	defer p.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case id := <-p.jobs:
			p.process(ctx, id)
		}
	}
}

func (p *Processor) process(ctx context.Context, id string) {
	notification, err := p.store.Get(ctx, id)
	if err != nil {
		return
	}

	notification, err = p.store.Update(ctx, id, func(n *domain.Notification) {
		n.Status = domain.StatusValidating
		n.FailureReason = ""
		n.ValidationError = ""
	})
	if err != nil {
		return
	}

	if err := p.validate(ctx, notification); err != nil {
		_, _ = p.store.Update(context.Background(), id, func(n *domain.Notification) {
			n.Status = domain.StatusRejected
			n.ValidationError = err.Error()
		})
		return
	}

	sender, ok := p.providers.Get(notification.Channel)
	if !ok {
		_, _ = p.store.Update(context.Background(), id, func(n *domain.Notification) {
			n.Status = domain.StatusFailed
			n.FailureReason = provider.ErrProviderNotFound.Error()
		})
		return
	}

	_, err = p.store.Update(ctx, id, func(n *domain.Notification) {
		n.Status = domain.StatusSending
		n.Provider = sender.Name()
	})
	if err != nil {
		return
	}

	if err := p.sendWithRetry(ctx, id, sender); err != nil {
		_, _ = p.store.Update(context.Background(), id, func(n *domain.Notification) {
			n.Status = domain.StatusFailed
			n.FailureReason = err.Error()
		})
	}
}

func (p *Processor) validate(ctx context.Context, notification domain.Notification) error {
	for _, validator := range p.validators {
		if err := validator.Validate(ctx, notification); err != nil {
			return fmt.Errorf("%s validation failed: %w", validator.Name(), err)
		}
	}
	return nil
}

func (p *Processor) sendWithRetry(ctx context.Context, id string, sender provider.Sender) error {
	var lastErr error
	for attempt := 1; attempt <= p.config.MaxAttempts; attempt++ {
		notification, err := p.store.Update(ctx, id, func(n *domain.Notification) {
			n.Attempts = attempt
		})
		if err != nil {
			return err
		}

		sendCtx, cancel := context.WithTimeout(ctx, p.config.SendTimeout)
		err = sender.Send(sendCtx, notification)
		cancel()
		if err == nil {
			deliveredAt := time.Now().UTC()
			_, updateErr := p.store.Update(context.Background(), id, func(n *domain.Notification) {
				n.Status = domain.StatusDelivered
				n.FailureReason = ""
				n.DeliveredAt = &deliveredAt
			})
			return updateErr
		}

		lastErr = err
		if attempt == p.config.MaxAttempts {
			break
		}

		if err := wait(ctx, p.config.RetryBackoff*time.Duration(attempt)); err != nil {
			return err
		}
	}
	return lastErr
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var _ NotificationStore = (*store.MemoryStore)(nil)
