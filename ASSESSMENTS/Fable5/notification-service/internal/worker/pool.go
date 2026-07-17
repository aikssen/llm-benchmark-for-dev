// Package worker implements the asynchronous processing of deliveries: a
// bounded queue drained by a fixed pool of workers. The bounded queue plus
// fixed worker count keep resource usage predictable under bursty traffic;
// when the queue is full the API applies backpressure instead of buffering
// without limit.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"notificationservice/internal/domain"
	"notificationservice/internal/provider"
	"notificationservice/internal/store"
	"notificationservice/internal/validation"
)

// ErrQueueFull is returned by EnqueueBatch when the queue cannot absorb the
// whole batch. Callers should surface it as backpressure (HTTP 429).
var ErrQueueFull = errors.New("delivery queue is full")

// Job identifies one delivery to process. Jobs carry only IDs; state lives
// in the store, which keeps the queue cheap and makes jobs trivially
// serializable if the queue is later moved to an external broker.
type Job struct {
	NotificationID string
	DeliveryID     string
}

// Config tunes the pool. All fields must be set (see config.Default).
type Config struct {
	// Workers is the number of concurrent delivery processors.
	Workers int
	// QueueCapacity bounds the number of queued (not yet processing) jobs.
	QueueCapacity int
	// MaxAttempts is the total number of provider send attempts per delivery
	// (first try included).
	MaxAttempts int
	// RetryBaseDelay is the backoff before the first retry; it doubles each
	// retry up to RetryMaxDelay, with jitter.
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
	// ValidationTimeout bounds the whole validation pipeline per delivery.
	ValidationTimeout time.Duration
	// SendTimeout bounds each individual provider send attempt.
	SendTimeout time.Duration
}

// Pool validates and delivers queued jobs.
type Pool struct {
	cfg       Config
	store     store.Store
	providers *provider.Registry
	pipeline  *validation.Pipeline
	logger    *slog.Logger

	queue chan Job
	// enqueueMu serializes producers so the capacity check in EnqueueBatch
	// is atomic for the whole batch (consumers only ever free space).
	enqueueMu sync.Mutex
	wg        sync.WaitGroup
}

// New builds a pool. Call Start to begin processing.
func New(cfg Config, st store.Store, providers *provider.Registry, pipeline *validation.Pipeline, logger *slog.Logger) *Pool {
	return &Pool{
		cfg:       cfg,
		store:     st,
		providers: providers,
		pipeline:  pipeline,
		logger:    logger,
		queue:     make(chan Job, cfg.QueueCapacity),
	}
}

// Start launches the workers. They run until ctx is cancelled; call Wait to
// block until all in-flight work has finished.
func (p *Pool) Start(ctx context.Context) {
	for range p.cfg.Workers {
		p.wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-p.queue:
					p.process(ctx, job)
				}
			}
		})
	}
}

// Wait blocks until all workers have exited (after the Start context is
// cancelled). Jobs still queued at shutdown are not processed; with the
// in-memory store they are lost, which is documented in the README.
func (p *Pool) Wait() { p.wg.Wait() }

// EnqueueBatch atomically enqueues all jobs or none. Returning ErrQueueFull
// without partial enqueues lets the API reject the whole request cleanly.
func (p *Pool) EnqueueBatch(jobs []Job) error {
	p.enqueueMu.Lock()
	defer p.enqueueMu.Unlock()

	if len(jobs) > cap(p.queue)-len(p.queue) {
		return fmt.Errorf("%w: capacity %d, queued %d, batch %d",
			ErrQueueFull, cap(p.queue), len(p.queue), len(jobs))
	}
	for _, j := range jobs {
		p.queue <- j // cannot block: capacity was reserved under enqueueMu
	}
	return nil
}

// process runs one delivery end to end: validate, send with retries, record
// the terminal status.
func (p *Pool) process(ctx context.Context, job Job) {
	logger := p.logger.With("notification_id", job.NotificationID, "delivery_id", job.DeliveryID)

	n, err := p.store.GetNotification(ctx, job.NotificationID)
	if err != nil {
		logger.Error("cannot load notification for queued job", "error", err)
		return
	}
	var delivery domain.Delivery
	found := false
	for _, d := range n.Deliveries {
		if d.ID == job.DeliveryID {
			delivery, found = d, true
			break
		}
	}
	if !found {
		logger.Error("queued delivery not found in notification")
		return
	}

	p.setStatus(ctx, job, domain.DeliveryProcessing, "")

	// Validation: independent checks, concurrent, first failure cancels the
	// rest. Validation failures are terminal; retrying would not fix them.
	in := validation.Input{Channel: n.Channel, Message: n.Message, Recipient: delivery.Recipient}
	vctx, cancel := context.WithTimeout(ctx, p.cfg.ValidationTimeout)
	err = p.pipeline.Run(vctx, in)
	cancel()
	if err != nil {
		logger.Info("delivery rejected by validation", "error", err)
		p.setStatus(ctx, job, domain.DeliveryFailed, err.Error())
		return
	}

	prov, err := p.providers.Get(n.Channel)
	if err != nil {
		logger.Error("no provider for channel", "channel", n.Channel, "error", err)
		p.setStatus(ctx, job, domain.DeliveryFailed, err.Error())
		return
	}

	p.send(ctx, job, n, delivery, prov, logger)
}

// send attempts delivery through the provider with exponential backoff and
// jitter, until success, a permanent error, attempt exhaustion, or shutdown.
func (p *Pool) send(ctx context.Context, job Job, n domain.Notification, d domain.Delivery, prov provider.Provider, logger *slog.Logger) {
	for attempt := 1; ; attempt++ {
		p.recordAttempt(ctx, job, attempt)

		sctx, cancel := context.WithTimeout(ctx, p.cfg.SendTimeout)
		err := prov.Send(sctx, n, d.Recipient)
		cancel()

		if err == nil {
			p.setStatus(ctx, job, domain.DeliveryDelivered, "")
			logger.Info("delivery succeeded", "provider", prov.Name(), "attempts", attempt)
			return
		}

		switch {
		case provider.IsPermanent(err):
			logger.Warn("delivery failed permanently", "provider", prov.Name(), "attempt", attempt, "error", err)
			p.setStatus(ctx, job, domain.DeliveryFailed, err.Error())
			return
		case attempt >= p.cfg.MaxAttempts:
			logger.Warn("delivery failed after exhausting attempts", "provider", prov.Name(), "attempts", attempt, "error", err)
			p.setStatus(ctx, job, domain.DeliveryFailed,
				fmt.Sprintf("gave up after %d attempts: %v", attempt, err))
			return
		}

		delay := backoff(p.cfg.RetryBaseDelay, p.cfg.RetryMaxDelay, attempt)
		logger.Info("delivery attempt failed, retrying", "provider", prov.Name(), "attempt", attempt, "retry_in", delay, "error", err)
		p.setStatus(ctx, job, domain.DeliveryProcessing, err.Error())

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			// Shutting down mid-retry: record the last error as terminal.
			p.setStatus(ctx, job, domain.DeliveryFailed,
				fmt.Sprintf("aborted by shutdown after %d attempts: %v", attempt, err))
			return
		case <-timer.C:
		}
	}
}

func (p *Pool) setStatus(ctx context.Context, job Job, status domain.DeliveryStatus, lastError string) {
	// Status writes must survive shutdown cancellation so terminal states are
	// recorded; hence WithoutCancel.
	_, err := p.store.UpdateDelivery(context.WithoutCancel(ctx), job.NotificationID, job.DeliveryID, func(d *domain.Delivery) {
		d.Status = status
		d.LastError = lastError
	})
	if err != nil {
		p.logger.Error("cannot update delivery status",
			"notification_id", job.NotificationID, "delivery_id", job.DeliveryID, "error", err)
	}
}

func (p *Pool) recordAttempt(ctx context.Context, job Job, attempt int) {
	_, err := p.store.UpdateDelivery(context.WithoutCancel(ctx), job.NotificationID, job.DeliveryID, func(d *domain.Delivery) {
		d.Attempts = attempt
	})
	if err != nil {
		p.logger.Error("cannot record delivery attempt",
			"notification_id", job.NotificationID, "delivery_id", job.DeliveryID, "error", err)
	}
}

// backoff returns the delay before the retry that follows the given attempt:
// base doubled per attempt, capped at max, with ±25% jitter to avoid
// synchronized retry storms against a struggling provider.
func backoff(base, max time.Duration, attempt int) time.Duration {
	d := base << (attempt - 1)
	if d > max || d <= 0 { // d <= 0 guards shift overflow
		d = max
	}
	jitter := time.Duration((rand.Float64() - 0.5) * 0.5 * float64(d))
	return d + jitter
}
