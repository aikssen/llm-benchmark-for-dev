// Package dispatcher moves notifications from the API to the providers.
//
// Accepted notifications are stored (status pending) and their IDs are
// pushed onto a bounded in-memory queue consumed by a fixed pool of
// workers. Bounds on both the queue and the pool keep resource usage
// predictable under bursty traffic: when the queue is full the API
// applies backpressure (HTTP 503 + Retry-After) instead of growing
// memory unboundedly.
package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"notifications/internal/domain"
	"notifications/internal/provider"
	"notifications/internal/store"
	"notifications/internal/validation"
)

// ErrQueueFull is returned when the processing queue has no capacity
// left; the API translates it into a 503 with Retry-After.
var ErrQueueFull = errors.New("processing queue is full")

// ErrStopped is returned when enqueueing after Stop was called.
var ErrStopped = errors.New("dispatcher is stopped")

// Dispatcher owns the queue and the worker pool.
type Dispatcher struct {
	store    store.Store
	pipeline *validation.Pipeline
	registry *provider.Registry
	logger   *slog.Logger
	queueCap int
	workerN  int

	queue   chan string
	mu      sync.Mutex // guards enqueue/stop coordination
	stopped bool

	wg       sync.WaitGroup
	stopOnce sync.Once
}

// New builds a Dispatcher. workerN workers will consume a queue with
// room for queueCap notification IDs.
func New(st store.Store, p *validation.Pipeline, r *provider.Registry, workerN, queueCap int, logger *slog.Logger) *Dispatcher {
	if workerN <= 0 {
		workerN = 1
	}
	if queueCap <= 0 {
		queueCap = 1
	}
	return &Dispatcher{
		store:    st,
		pipeline: p,
		registry: r,
		logger:   logger,
		workerN:  workerN,
		queueCap: queueCap,
		queue:    make(chan string, queueCap),
	}
}

// QueueCapacity exposes the configured queue capacity.
func (d *Dispatcher) QueueCapacity() int { return d.queueCap }

// Start launches the worker pool. Workers run until Stop is called and
// the queue has been drained.
func (d *Dispatcher) Start() {
	for i := 0; i < d.workerN; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}
}

// EnqueueBatch atomically reserves queue capacity for all ids and pushes
// them. All-or-nothing: either every ID is queued or none is, so the API
// never accepts a request it cannot fully process.
func (d *Dispatcher) EnqueueBatch(ids []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return ErrStopped
	}
	if len(d.queue)+len(ids) > d.queueCap {
		return ErrQueueFull
	}
	for _, id := range ids {
		d.queue <- id
	}
	return nil
}

// Stop closes the queue and waits for workers to drain it, or until ctx
// expires. Notifications still pending after the deadline remain stored
// as pending (a durable store plus a re-enqueuer would pick them up; see
// README, "Future improvements").
func (d *Dispatcher) Stop(ctx context.Context) error {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.stopped = true
		d.mu.Unlock()
		close(d.queue)
	})

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) worker(id int) {
	defer d.wg.Done()
	log := d.logger.With("worker", id)
	for notifID := range d.queue {
		// Workers use a fresh background context: processing must not be
		// tied to the lifecycle of the HTTP request that queued the work.
		if err := d.process(context.Background(), notifID); err != nil {
			log.Warn("processing finished with error", "notification", notifID, "error", err)
		}
	}
}

// process runs the full lifecycle of one notification: validate, then
// deliver. Every state transition is persisted.
func (d *Dispatcher) process(ctx context.Context, id string) error {
	n, err := d.store.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("load notification: %w", err)
	}
	log := d.logger.With("notification", n.ID, "channel", n.Channel, "recipient", n.Recipient)

	if n.Status.Terminal() {
		return nil // already processed (e.g. duplicate enqueue)
	}

	// ---- Validation phase ---------------------------------------------
	d.transition(ctx, n, domain.StatusValidating, "")
	if err := d.pipeline.Validate(ctx, n); err != nil {
		d.transition(ctx, n, domain.StatusRejected, validation.RejectionSummary(err))
		log.Info("notification rejected", "reason", n.LastError)
		return nil // rejection is a normal outcome, not a processing error
	}

	// ---- Delivery phase -------------------------------------------------
	p, ok := d.registry.Get(n.Channel)
	if !ok {
		d.transition(ctx, n, domain.StatusFailed, "no provider registered for channel "+string(n.Channel))
		return nil
	}
	d.transition(ctx, n, domain.StatusSending, "")
	if err := p.Send(ctx, n); err != nil {
		d.transition(ctx, n, domain.StatusFailed, err.Error())
		log.Warn("delivery failed", "attempts", n.Attempts, "error", err)
		return nil
	}

	d.transition(ctx, n, domain.StatusDelivered, "")
	log.Info("notification delivered", "attempts", n.Attempts)
	return nil
}

// transition persists a status change.
func (d *Dispatcher) transition(ctx context.Context, n *domain.Notification, status domain.Status, lastErr string) {
	n.Status = status
	if lastErr != "" {
		n.LastError = lastErr
	}
	n.UpdatedAt = time.Now()
	if err := d.store.Upsert(ctx, n); err != nil {
		d.logger.Error("failed to persist status transition",
			"notification", n.ID, "status", status, "error", err)
	}
}
