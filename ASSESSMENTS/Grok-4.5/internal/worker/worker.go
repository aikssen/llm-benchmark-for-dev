package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
	"github.com/evercifuentes/notifications/internal/provider"
	"github.com/evercifuentes/notifications/internal/store"
	"github.com/evercifuentes/notifications/internal/validation"
)

// Job is a unit of work for the delivery pipeline.
type Job struct {
	NotificationID string
}

// Pool processes notification jobs with bounded concurrency.
// Bounded queue size keeps memory usage predictable during traffic bursts.
type Pool struct {
	store      store.Store
	validators *validation.Pipeline
	providers  *provider.Registry
	jobs       chan Job
	workers    int
	maxRetries int
	baseDelay  time.Duration
	log        *slog.Logger
	wg         sync.WaitGroup
}

// Config configures the worker pool.
type Config struct {
	Workers    int
	QueueSize  int
	MaxRetries int
	BaseDelay  time.Duration
	Logger     *slog.Logger
}

// NewPool creates a worker pool.
func NewPool(
	st store.Store,
	validators *validation.Pipeline,
	providers *provider.Registry,
	cfg Config,
) *Pool {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 200 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Pool{
		store:      st,
		validators: validators,
		providers:  providers,
		jobs:       make(chan Job, cfg.QueueSize),
		workers:    cfg.Workers,
		maxRetries: cfg.MaxRetries,
		baseDelay:  cfg.BaseDelay,
		log:        cfg.Logger,
	}
}

// Start launches worker goroutines.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.loop(ctx, i)
	}
}

// Stop waits for workers to finish after the jobs channel is closed or context cancelled.
func (p *Pool) Stop() {
	p.wg.Wait()
}

// Enqueue submits a job. Returns false if the queue is full (backpressure).
func (p *Pool) Enqueue(ctx context.Context, job Job) bool {
	select {
	case <-ctx.Done():
		return false
	case p.jobs <- job:
		return true
	default:
		// Non-blocking: apply backpressure when saturated.
		select {
		case <-ctx.Done():
			return false
		case p.jobs <- job:
			return true
		case <-time.After(50 * time.Millisecond):
			return false
		}
	}
}

func (p *Pool) loop(ctx context.Context, id int) {
	defer p.wg.Done()
	p.log.Info("worker started", "worker_id", id)
	for {
		select {
		case <-ctx.Done():
			p.log.Info("worker stopped", "worker_id", id)
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			p.process(ctx, job)
		}
	}
}

func (p *Pool) process(ctx context.Context, job Job) {
	n, err := p.store.Get(ctx, job.NotificationID)
	if err != nil {
		p.log.Error("load notification", "id", job.NotificationID, "error", err)
		return
	}

	n.Status = domain.StatusValidating
	_ = p.store.Update(ctx, n)

	if err := p.validators.Validate(ctx, n); err != nil {
		n.Status = domain.StatusRejected
		n.LastError = err.Error()
		_ = p.store.Update(ctx, n)
		p.log.Info("notification rejected", "id", n.ID, "error", err)
		return
	}

	n.Status = domain.StatusQueued
	_ = p.store.Update(ctx, n)

	prov, err := p.providers.Get(n.Channel)
	if err != nil {
		n.Status = domain.StatusFailed
		n.LastError = err.Error()
		_ = p.store.Update(ctx, n)
		return
	}

	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if ctx.Err() != nil {
			n.Status = domain.StatusFailed
			n.LastError = ctx.Err().Error()
			_ = p.store.Update(ctx, n)
			return
		}

		n.Status = domain.StatusSending
		n.Attempts = attempt + 1
		_ = p.store.Update(ctx, n)

		sendErr := prov.Send(ctx, n)
		if sendErr == nil {
			n.Status = domain.StatusDelivered
			n.LastError = ""
			_ = p.store.Update(ctx, n)
			p.log.Info("notification delivered", "id", n.ID, "attempts", n.Attempts)
			return
		}

		n.LastError = sendErr.Error()
		_ = p.store.Update(ctx, n)

		if !errors.Is(sendErr, provider.ErrTemporary) || attempt == p.maxRetries {
			n.Status = domain.StatusFailed
			_ = p.store.Update(ctx, n)
			p.log.Warn("notification failed", "id", n.ID, "attempts", n.Attempts, "error", sendErr)
			return
		}

		delay := p.baseDelay * time.Duration(1<<attempt)
		p.log.Info("retrying delivery", "id", n.ID, "attempt", attempt+1, "delay", delay)
		select {
		case <-ctx.Done():
			n.Status = domain.StatusFailed
			n.LastError = ctx.Err().Error()
			_ = p.store.Update(ctx, n)
			return
		case <-time.After(delay):
		}
	}
}
