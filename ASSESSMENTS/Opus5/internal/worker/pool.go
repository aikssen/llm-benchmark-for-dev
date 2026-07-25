// Package worker runs the fixed-size pool that drains the job queue.
//
// A fixed pool is what makes resource usage predictable: no matter how large
// a burst is, the number of concurrent provider calls, goroutines and open
// sockets is bounded by the pool size, and the excess waits in the bounded
// queue instead of being spawned.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/evercifuentes/notifier/internal/queue"
)

// Processor handles a single job. Implementations must not panic; the pool
// recovers anyway so that one bad job cannot take a worker down.
type Processor interface {
	Process(ctx context.Context, job queue.Job)
}

// Pool consumes jobs from a queue with a fixed number of goroutines.
type Pool struct {
	queue     *queue.Queue
	processor Processor
	size      int
	logger    *slog.Logger

	wg      sync.WaitGroup
	started bool
	mu      sync.Mutex
	cancel  context.CancelFunc
}

// New builds a pool of the given size (at least 1).
func New(q *queue.Queue, p Processor, size int, logger *slog.Logger) *Pool {
	return &Pool{
		queue:     q,
		processor: p,
		size:      max(size, 1),
		logger:    logger,
	}
}

// Size returns the number of workers.
func (p *Pool) Size() int { return p.size }

// Start launches the workers. The derived context is cancelled by Stop, which
// is how in-flight provider calls are interrupted on a hard shutdown.
func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	p.started = true

	workerCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	for i := range p.size {
		p.wg.Add(1)
		go p.run(workerCtx, i)
	}
	p.logger.Info("worker pool started", slog.Int("workers", p.size), slog.Int("queue_capacity", p.queue.Capacity()))
}

func (p *Pool) run(ctx context.Context, id int) {
	defer p.wg.Done()

	log := p.logger.With(slog.Int("worker", id))
	for job := range p.queue.Jobs() {
		// Free the queue slot as soon as the job is picked up: the queue
		// measures work that is waiting, not work in progress.
		p.queue.Ack()

		if ctx.Err() != nil {
			// Shutting down. The job stays in its current state and is
			// reported as such; see docs/DESIGN.md on durability.
			log.Warn("dropping job, shutting down",
				slog.String("notification_id", job.NotificationID),
				slog.String("delivery_id", job.DeliveryID))
			continue
		}
		p.process(ctx, log, job)
	}
}

// process isolates one job so a panic cannot kill the worker.
func (p *Pool) process(ctx context.Context, log *slog.Logger, job queue.Job) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("recovered from panic while processing job",
				slog.Any("panic", r),
				slog.String("notification_id", job.NotificationID),
				slog.String("delivery_id", job.DeliveryID))
		}
	}()
	p.processor.Process(ctx, job)
}

// Stop drains the queue and waits for the workers to finish.
//
// It closes the queue first so no new work is admitted, then waits for the
// backlog to be processed. If ctx expires first, in-flight work is cancelled
// and the error is returned so the caller can report a non-clean shutdown.
func (p *Pool) Stop(ctx context.Context) error {
	p.queue.Close()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("worker pool stopped cleanly")
		return nil
	case <-ctx.Done():
		// Out of time: interrupt in-flight provider calls and wait for the
		// workers to unwind so we do not leak goroutines past shutdown.
		p.mu.Lock()
		cancel := p.cancel
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		<-done
		p.logger.Warn("worker pool stopped before draining the queue")
		return errors.New("worker pool did not drain before the shutdown deadline")
	}
}
