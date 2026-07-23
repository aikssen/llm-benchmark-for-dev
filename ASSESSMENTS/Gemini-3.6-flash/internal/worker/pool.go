package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"notification-service/internal/domain"
)

var ErrQueueFull = errors.New("worker queue is full, unable to enqueue job")

type JobHandler func(ctx context.Context, item *domain.SingleRecipientItem) error

type Pool struct {
	numWorkers int
	queue      chan *domain.SingleRecipientItem
	handler    JobHandler
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	activeJobs int64
	totalJobs  uint64
}

func NewPool(numWorkers int, queueCapacity int, handler JobHandler) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		numWorkers: numWorkers,
		queue:      make(chan *domain.SingleRecipientItem, queueCapacity),
		handler:    handler,
		ctx:        ctx,
		cancel:     cancel,
	}

	p.start()
	return p
}

func (p *Pool) start() {
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			for {
				select {
				case <-p.ctx.Done():
					// Process remaining items in queue before exiting
					p.drainQueue(workerID)
					return
				case item, ok := <-p.queue:
					if !ok {
						return
					}
					p.processItem(workerID, item)
				}
			}
		}(i + 1)
	}
}

func (p *Pool) processItem(workerID int, item *domain.SingleRecipientItem) {
	atomic.AddInt64(&p.activeJobs, 1)
	atomic.AddUint64(&p.totalJobs, 1)
	defer atomic.AddInt64(&p.activeJobs, -1)

	if err := p.handler(p.ctx, item); err != nil {
		slog.Error("Worker failed to process notification item",
			"worker_id", workerID,
			"item_id", item.ID,
			"recipient", item.Recipient,
			"error", err,
		)
	}
}

func (p *Pool) drainQueue(workerID int) {
	for item := range p.queue {
		p.processItem(workerID, item)
	}
}

// Submit enqueues an item to the worker pool.
func (p *Pool) Submit(item *domain.SingleRecipientItem) error {
	select {
	case <-p.ctx.Done():
		return errors.New("worker pool is shutting down")
	case p.queue <- item:
		return nil
	default:
		return ErrQueueFull
	}
}

// Stats returns current pool metrics.
func (p *Pool) QueueLength() int {
	return len(p.queue)
}

func (p *Pool) ActiveJobs() int64 {
	return atomic.LoadInt64(&p.activeJobs)
}

func (p *Pool) TotalJobs() uint64 {
	return atomic.LoadUint64(&p.totalJobs)
}

// Stop gracefully shuts down the worker pool, waiting for queued jobs to finish.
func (p *Pool) Stop() {
	p.cancel()
	close(p.queue)
	p.wg.Wait()
}
