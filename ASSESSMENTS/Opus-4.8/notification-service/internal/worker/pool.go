// Package worker provides a bounded worker pool that decouples request
// acceptance from notification processing.
//
// The pool is the mechanism for the assessment's "bursts of incoming requests"
// with "predictable resource usage": a fixed number of workers drain a bounded
// queue. Bursts are absorbed by the queue buffer; sustained overload applies
// backpressure (Submit blocks until there is room or the caller's context
// expires), so memory and goroutine counts stay bounded instead of growing with
// load.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// ErrPoolClosed is returned by Submit after the pool has begun shutting down.
var ErrPoolClosed = errors.New("worker pool is closed")

// Handler processes a single queued job, identified by its notification ID. It
// receives the pool's context, which is cancelled if a graceful shutdown times
// out, so long-running work can abort.
type Handler func(ctx context.Context, notificationID string)

// Pool is a fixed-size worker pool fed by a bounded queue.
type Pool struct {
	jobs    chan string
	closing chan struct{}
	workers int
	handler Handler
	log     *slog.Logger

	wg         sync.WaitGroup
	closeOnce  sync.Once
	baseCtx    context.Context
	cancelBase context.CancelFunc
}

// New creates a pool with the given number of workers and queue capacity. Both
// are clamped to at least 1. The handler is invoked once per job.
func New(workers, queueSize int, handler Handler, log *slog.Logger) *Pool {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	if log == nil {
		log = slog.Default()
	}
	return &Pool{
		jobs:    make(chan string, queueSize),
		closing: make(chan struct{}),
		workers: workers,
		handler: handler,
		log:     log,
	}
}

// Start launches the worker goroutines. The provided context is the parent for
// all handler invocations; cancelling it stops the pool. Start returns
// immediately.
func (p *Pool) Start(ctx context.Context) {
	p.baseCtx, p.cancelBase = context.WithCancel(ctx)
	for range p.workers {
		p.wg.Add(1)
		go p.run()
	}
	p.log.Info("worker pool started",
		slog.Int("workers", p.workers),
		slog.Int("queue_size", cap(p.jobs)),
	)
}

// Submit enqueues a job. It blocks while the queue is full (backpressure) and
// returns when the job is queued, the context is cancelled, or the pool is
// closing. This bounds in-flight work: callers cannot outrun the workers without
// either waiting or being told to back off.
func (p *Pool) Submit(ctx context.Context, notificationID string) error {
	// Fast path: reject immediately if we are already shutting down.
	select {
	case <-p.closing:
		return ErrPoolClosed
	default:
	}

	select {
	case p.jobs <- notificationID:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closing:
		return ErrPoolClosed
	}
}

// QueueDepth returns the number of jobs currently buffered. Useful for metrics
// and health checks.
func (p *Pool) QueueDepth() int { return len(p.jobs) }

func (p *Pool) run() {
	defer p.wg.Done()
	for {
		select {
		case job := <-p.jobs:
			p.handler(p.baseCtx, job)
		case <-p.closing:
			// Shutting down: drain any remaining buffered jobs, then exit.
			p.drain()
			return
		case <-p.baseCtx.Done():
			// Hard stop (parent cancelled): abandon the queue immediately.
			return
		}
	}
}

// drain processes whatever is left in the queue without blocking. Each worker
// runs its own drain loop on shutdown, so the buffered jobs are shared out
// across all workers.
func (p *Pool) drain() {
	for {
		select {
		case job := <-p.jobs:
			p.handler(p.baseCtx, job)
		default:
			return
		}
	}
}

// Shutdown stops accepting new jobs and waits for in-flight and buffered jobs to
// finish, up to the deadline on ctx. If the deadline is hit first, it cancels
// the workers' context to abort in-flight work and returns ctx.Err().
func (p *Pool) Shutdown(ctx context.Context) error {
	p.closeOnce.Do(func() { close(p.closing) })

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancelBase()
		p.log.Info("worker pool drained cleanly")
		return nil
	case <-ctx.Done():
		// Grace period elapsed: force-cancel in-flight work.
		p.cancelBase()
		p.log.Warn("worker pool shutdown timed out; forced cancellation")
		p.wg.Wait()
		return ctx.Err()
	}
}
