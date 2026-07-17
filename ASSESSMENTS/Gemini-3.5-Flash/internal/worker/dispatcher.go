package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"notification-service/internal/service"
)

// Dispatcher manages worker goroutines to process notifications asynchronously
type Dispatcher struct {
	queue        chan string
	workerCount  int
	orchestrator *service.Orchestrator
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	running      bool
	mu           sync.Mutex
}

// NewDispatcher creates a new worker pool Dispatcher
func NewDispatcher(workerCount int, queueBuffer int, orchestrator *service.Orchestrator) *Dispatcher {
	return &Dispatcher{
		queue:        make(chan string, queueBuffer),
		workerCount:  workerCount,
		orchestrator: orchestrator,
	}
}

// Start spawns the worker goroutines
func (d *Dispatcher) Start(parentCtx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		return
	}

	d.ctx, d.cancel = context.WithCancel(parentCtx)
	d.running = true

	for i := 1; i <= d.workerCount; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}
	slog.Info("Dispatcher started", "workers", d.workerCount, "buffer_size", cap(d.queue))
}

// Enqueue puts a notification ID on the processing queue
func (d *Dispatcher) Enqueue(ctx context.Context, id string) error {
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()

	if !running {
		return errors.New("dispatcher is not running")
	}

	select {
	case d.queue <- id:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop gracefully shuts down the worker pool, draining the queue.
// If the shutdown context expires, it cancels active worker contexts.
func (d *Dispatcher) Stop(ctx context.Context) error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}
	d.running = false
	d.mu.Unlock()

	// Close queue to stop workers when all jobs are drained
	close(d.queue)

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.cancel() // Clean up context resources
		slog.Info("Dispatcher stopped gracefully")
		return nil
	case <-ctx.Done():
		// Grace period expired, abort all running handlers
		d.cancel()
		<-done // Wait for workers to clean up after cancellation
		slog.Warn("Dispatcher shutdown timed out; forced stop")
		return ctx.Err()
	}
}

func (d *Dispatcher) worker(id int) {
	defer d.wg.Done()
	slog.Debug("Worker started", "worker_id", id)

	for jobID := range d.queue {
		slog.Info("Worker processing notification", "worker_id", id, "notification_id", jobID)
		err := d.orchestrator.ProcessNotification(d.ctx, jobID)
		if err != nil {
			slog.Error("Worker failed to process notification", "worker_id", id, "notification_id", jobID, "error", err)
		} else {
			slog.Info("Worker successfully processed notification", "worker_id", id, "notification_id", jobID)
		}
	}
	slog.Debug("Worker stopped", "worker_id", id)
}
