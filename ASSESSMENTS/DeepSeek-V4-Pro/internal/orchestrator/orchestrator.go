package orchestrator

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/assessment/notify-service/internal/domain"
	"github.com/assessment/notify-service/internal/provider"
	"github.com/assessment/notify-service/internal/store"
	"github.com/assessment/notify-service/internal/validator"
)

type Config struct {
	MaxWorkers    int
	MaxRetries    int
	RetryBackoff  time.Duration
	RequestBuffer int
}

type Orchestrator struct {
	cfg       Config
	store     store.Store
	registry  *provider.Registry
	pipeline  *validator.Pipeline
	jobCh     chan *domain.Notification
	wg        sync.WaitGroup
}

func New(cfg Config, st store.Store, reg *provider.Registry, pipe *validator.Pipeline) *Orchestrator {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 10
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 300 * time.Millisecond
	}
	if cfg.RequestBuffer <= 0 {
		cfg.RequestBuffer = 100
	}

	o := &Orchestrator{
		cfg:      cfg,
		store:     st,
		registry:  reg,
		pipeline:  pipe,
		jobCh:     make(chan *domain.Notification, cfg.RequestBuffer),
	}

	for i := 0; i < cfg.MaxWorkers; i++ {
		o.wg.Add(1)
		go o.worker()
	}

	return o
}

func (o *Orchestrator) Submit(ctx context.Context, n *domain.Notification) error {
	n.Status = domain.StatusPending
	if err := o.store.Save(ctx, n); err != nil {
		return err
	}

	select {
	case o.jobCh <- n:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *Orchestrator) Shutdown() {
	close(o.jobCh)
	o.wg.Wait()
}

func (o *Orchestrator) worker() {
	defer o.wg.Done()
	for n := range o.jobCh {
		o.processJob(n)
	}
}

func (o *Orchestrator) processJob(n *domain.Notification) {
	ctx := context.Background()

	n.Status = domain.StatusValidating
	_ = o.store.Save(ctx, n)

	result := o.pipeline.Validate(ctx, n)
	if !result.Passed {
		n.Status = domain.StatusValidationErr
		n.ValidationErrors = result.Errors
		_ = o.store.Save(ctx, n)
		return
	}

	n.Status = domain.StatusSending
	_ = o.store.Save(ctx, n)

	prov, err := o.registry.Get(n.Channel)
	if err != nil {
		n.Status = domain.StatusFailed
		n.ValidationErrors = []string{err.Error()}
		_ = o.store.Save(ctx, n)
		return
	}

	n.DeliveryResults = make([]domain.DeliveryResult, len(n.Recipients))
	failures := 0

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i, r := range n.Recipients {
		wg.Add(1)
		i := i
		r := r
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			result := o.sendWithRetry(ctx, prov, r, n.Message)

			mu.Lock()
			n.DeliveryResults[i] = result
			if !result.Success {
				failures++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	switch {
	case failures == 0:
		n.Status = domain.StatusSent
	case failures == len(n.Recipients):
		n.Status = domain.StatusFailed
	default:
		n.Status = domain.StatusPartialSent
	}

	_ = o.store.Save(ctx, n)
}

func (o *Orchestrator) sendWithRetry(ctx context.Context, prov domain.Provider, recipient domain.Recipient, message domain.Message) domain.DeliveryResult {
	result := domain.DeliveryResult{
		RecipientID: recipient.ID,
	}

	for attempt := 0; attempt <= o.cfg.MaxRetries; attempt++ {
		err := prov.Send(ctx, recipient, message)
		if err == nil {
			result.Success = true
			result.SentAt = time.Now()
			return result
		}

		result.Error = err.Error()
		log.Printf("delivery attempt %d/%d to %s via %s failed: %v",
			attempt+1, o.cfg.MaxRetries+1, recipient.ID, prov.Name(), err)

		if attempt < o.cfg.MaxRetries {
			backoff := o.cfg.RetryBackoff * time.Duration(1<<attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				result.Error = ctx.Err().Error()
				return result
			}
		}
	}

	return result
}
