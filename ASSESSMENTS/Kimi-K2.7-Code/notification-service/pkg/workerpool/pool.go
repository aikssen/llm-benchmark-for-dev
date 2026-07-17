package workerpool

import (
	"context"
	"sync"
	"time"
)

// JobHandler processes a single job.
type JobHandler func(ctx context.Context, job string)

// Pool is a bounded worker pool that processes notification IDs.
// It provides:
//   - predictable resource usage (fixed worker count, bounded queue)
//   - graceful shutdown
//   - a lightweight RunAsync helper for concurrent subtasks (e.g. validators)
type Pool struct {
	workers   int
	queueSize int
	jobs      chan string
	tasks     chan func()
	stopOnce  sync.Once
	stopCh    chan struct{}
	stopped   bool
	mu        sync.RWMutex
	wg        sync.WaitGroup
}

// NewPool creates a worker pool.
func NewPool(workers, queueSize int) *Pool {
	if workers <= 0 {
		workers = 4
	}
	if queueSize <= 0 {
		queueSize = 1000
	}
	return &Pool{
		workers:   workers,
		queueSize: queueSize,
		jobs:      make(chan string, queueSize),
		tasks:     make(chan func(), queueSize*2),
		stopCh:    make(chan struct{}),
	}
}

// Start launches the workers and begins processing jobs.
func (p *Pool) Start(ctx context.Context, handler JobHandler) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, handler)
	}
}

// Enqueue adds a job to the queue. It returns false when the pool is stopped.
func (p *Pool) Enqueue(job string) bool {
	p.mu.RLock()
	stopped := p.stopped
	p.mu.RUnlock()
	if stopped {
		return false
	}
	select {
	case p.jobs <- job:
		return true
	case <-p.stopCh:
		return false
	}
}

// RunAsync schedules a one-off function on the pool. Used for validators and provider calls.
func (p *Pool) RunAsync(fn func()) bool {
	p.mu.RLock()
	stopped := p.stopped
	p.mu.RUnlock()
	if stopped {
		return false
	}
	select {
	case p.tasks <- fn:
		return true
	case <-p.stopCh:
		return false
	}
}

// Shutdown signals the pool to stop and waits up to timeout for workers to finish.
func (p *Pool) Shutdown(timeout time.Duration) {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopped = true
		p.mu.Unlock()
		close(p.stopCh)
	})

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func (p *Pool) worker(ctx context.Context, handler JobHandler) {
	defer p.wg.Done()
	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			handler(ctx, job)
		case task, ok := <-p.tasks:
			if !ok {
				return
			}
			if task != nil {
				task()
			}
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}
