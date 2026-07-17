package delivery

import (
	"context"
	"log/slog"
	"sync"

	"github.com/aikssen/notification-service/internal/domain"
)

// WorkerPool bounds how much delivery work runs concurrently: a fixed
// number of goroutines drain a fixed-capacity channel. This gives
// predictable resource usage under bursty traffic — instead of spawning a
// goroutine per notification (unbounded), the queue simply absorbs the
// burst up to its capacity and workers drain it at a steady rate. Once
// the queue is full, Enqueue fails fast so the caller (the HTTP handler)
// can surface backpressure to the client rather than blocking or
// buffering without limit.
type WorkerPool struct {
	jobs       chan string
	dispatcher *Dispatcher
	size       int
	logger     *slog.Logger

	wg sync.WaitGroup
}

// NewWorkerPool builds a pool with the given number of workers and queue
// capacity. Call Start to begin processing and Shutdown to drain.
func NewWorkerPool(size, capacity int, dispatcher *Dispatcher, logger *slog.Logger) *WorkerPool {
	return &WorkerPool{
		jobs:       make(chan string, capacity),
		dispatcher: dispatcher,
		size:       size,
		logger:     logger,
	}
}

// Start launches the worker goroutines. They run until ctx is cancelled
// or the job channel is closed by Shutdown.
func (wp *WorkerPool) Start(ctx context.Context) {
	for i := 0; i < wp.size; i++ {
		wp.wg.Add(1)
		go wp.worker(ctx)
	}
}

func (wp *WorkerPool) worker(ctx context.Context) {
	defer wp.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-wp.jobs:
			if !ok {
				return
			}
			if err := wp.dispatcher.Deliver(ctx, id); err != nil {
				wp.logger.Error("notification delivery failed permanently", "notification_id", id, "error", err)
			}
		}
	}
}

// Enqueue schedules notification id for delivery. It never blocks: if the
// queue is at capacity it returns domain.ErrQueueFull immediately.
func (wp *WorkerPool) Enqueue(id string) error {
	select {
	case wp.jobs <- id:
		return nil
	default:
		return domain.ErrQueueFull
	}
}

// Shutdown closes the job queue and waits for in-flight deliveries to
// finish, or for ctx to be cancelled, whichever comes first.
func (wp *WorkerPool) Shutdown(ctx context.Context) error {
	close(wp.jobs)
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
