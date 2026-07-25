package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/platform/log"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/worker"
)

// processorFunc adapts a function to the worker.Processor interface.
type processorFunc func(ctx context.Context, job queue.Job)

func (f processorFunc) Process(ctx context.Context, job queue.Job) { f(ctx, job) }

// publish reserves and enqueues n jobs, failing the test if the queue refuses.
func publish(t *testing.T, q *queue.Queue, n int) {
	t.Helper()
	if err := q.Reserve(n); err != nil {
		t.Fatalf("Reserve(%d) = %v", n, err)
	}
	for i := range n {
		if err := q.Publish(queue.Job{NotificationID: "ntf_1", DeliveryID: string(rune('a' + i%26))}); err != nil {
			t.Fatalf("Publish() = %v", err)
		}
	}
}

func TestPoolProcessesEveryQueuedJob(t *testing.T) {
	t.Parallel()

	const jobs = 50
	q := queue.New(jobs)

	var processed atomic.Int32
	pool := worker.New(q, processorFunc(func(context.Context, queue.Job) {
		processed.Add(1)
	}), 4, log.Discard())

	pool.Start(context.Background())
	publish(t, q, jobs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v, want a clean drain", err)
	}

	if got := processed.Load(); got != jobs {
		t.Errorf("processed %d jobs, want %d", got, jobs)
	}
}

// The pool size is the cap on concurrent provider calls; without it a burst
// would translate directly into unbounded downstream load.
func TestPoolBoundsConcurrency(t *testing.T) {
	t.Parallel()

	const (
		size = 3
		jobs = 30
	)
	q := queue.New(jobs)

	var (
		mu      sync.Mutex
		current int
		peak    int
	)
	pool := worker.New(q, processorFunc(func(context.Context, queue.Job) {
		mu.Lock()
		current++
		peak = max(peak, current)
		mu.Unlock()

		time.Sleep(time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()
	}), size, log.Discard())

	pool.Start(context.Background())
	publish(t, q, jobs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > size {
		t.Errorf("peak concurrency was %d, want at most the pool size of %d", peak, size)
	}
}

// A worker that dies on a bad job takes throughput with it, so the pool
// isolates each job.
func TestPoolSurvivesAPanickingJob(t *testing.T) {
	t.Parallel()

	q := queue.New(10)

	var processed atomic.Int32
	pool := worker.New(q, processorFunc(func(_ context.Context, job queue.Job) {
		if job.DeliveryID == "a" {
			panic("provider integration blew up")
		}
		processed.Add(1)
	}), 1, log.Discard())

	pool.Start(context.Background())
	publish(t, q, 5) // the first job panics, the rest must still run

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}

	if got := processed.Load(); got != 4 {
		t.Errorf("processed %d jobs after a panic, want the remaining 4", got)
	}
}

// Shutdown must drain the accepted backlog rather than discard it.
func TestStopDrainsTheBacklog(t *testing.T) {
	t.Parallel()

	const jobs = 20
	q := queue.New(jobs)

	var processed atomic.Int32
	pool := worker.New(q, processorFunc(func(context.Context, queue.Job) {
		time.Sleep(2 * time.Millisecond)
		processed.Add(1)
	}), 2, log.Discard())

	pool.Start(context.Background())
	publish(t, q, jobs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if got := processed.Load(); got != jobs {
		t.Errorf("processed %d of %d jobs during shutdown, want all of them", got, jobs)
	}
}

// When the drain cannot finish in time, Stop must report it rather than
// hanging or lying about a clean shutdown.
func TestStopReportsAnIncompleteDrain(t *testing.T) {
	t.Parallel()

	q := queue.New(10)

	// The processor blocks until the test releases it, so the drain cannot
	// finish within the shutdown deadline.
	release := make(chan struct{})
	pool := worker.New(q, processorFunc(func(ctx context.Context, _ queue.Job) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}), 1, log.Discard())

	pool.Start(context.Background())
	publish(t, q, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := pool.Stop(ctx)
	close(release)

	if err == nil {
		t.Fatal("Stop() = nil, want an error when the deadline passes before the drain finishes")
	}
}

func TestStartIsIdempotent(t *testing.T) {
	t.Parallel()

	q := queue.New(10)
	var processed atomic.Int32
	pool := worker.New(q, processorFunc(func(context.Context, queue.Job) {
		processed.Add(1)
	}), 2, log.Discard())

	ctx := context.Background()
	pool.Start(ctx)
	pool.Start(ctx) // must not double the pool
	if got := pool.Size(); got != 2 {
		t.Errorf("Size() = %d, want 2", got)
	}

	publish(t, q, 4)

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if got := processed.Load(); got != 4 {
		t.Errorf("processed %d jobs, want 4", got)
	}
}

func TestPoolSizeIsAtLeastOne(t *testing.T) {
	t.Parallel()

	pool := worker.New(queue.New(1), processorFunc(func(context.Context, queue.Job) {}), 0, log.Discard())
	if got := pool.Size(); got != 1 {
		t.Errorf("Size() = %d, want it clamped to 1", got)
	}
}
