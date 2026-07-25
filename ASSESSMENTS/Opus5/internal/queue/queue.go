// Package queue is the bounded hand-off between the HTTP request path and
// the worker pool.
//
// Bounded is the important word. Under a burst, an unbounded queue converts a
// traffic spike into unbounded memory growth and ever-growing latency; a
// bounded one lets the service reject work it cannot service, immediately and
// honestly, while the accepted work keeps flowing.
package queue

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
)

var (
	// ErrFull means the queue has no room right now; the caller should retry.
	ErrFull = errors.New("queue is full")
	// ErrClosed means the queue is shutting down and accepts no new work.
	ErrClosed = errors.New("queue is closed")
	// ErrTooLarge means the request could never fit, regardless of load.
	ErrTooLarge = errors.New("request exceeds queue capacity")
)

// Job is one unit of work: deliver one message to one recipient.
//
// It is a self-contained snapshot rather than a pair of IDs. That keeps the
// hot path free of storage reads — a 5,000-recipient request would otherwise
// re-read the same aggregate 5,000 times — and mirrors what a message on a
// real broker would carry.
type Job struct {
	NotificationID string
	DeliveryID     string
	Channel        domain.Channel
	Recipient      domain.Recipient
	Message        domain.Message
	Metadata       map[string]string
	EnqueuedAt     time.Time
}

// Queue is a bounded FIFO of jobs with all-or-nothing admission.
//
// Admission is two-phase on purpose. A notification with N recipients calls
// Reserve(N) once: either the whole request is accepted, or none of it is.
// Publishing half a request and then failing would leave the caller with a
// notification that is silently missing recipients.
type Queue struct {
	jobs     chan Job
	capacity int

	mu     sync.Mutex
	depth  int // reserved slots not yet consumed by a worker
	closed bool
}

// New builds a queue with the given capacity (at least 1).
func New(capacity int) *Queue {
	capacity = max(capacity, 1)
	return &Queue{
		jobs:     make(chan Job, capacity),
		capacity: capacity,
	}
}

// Capacity returns the configured bound.
func (q *Queue) Capacity() int { return q.capacity }

// Depth returns the number of slots currently reserved or queued.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.depth
}

// Reserve claims n slots, all or nothing.
//
// A successful Reserve must be balanced by exactly n calls to Publish, or by
// Release for the slots that are not used.
func (q *Queue) Reserve(n int) error {
	if n <= 0 {
		return nil
	}
	if n > q.capacity {
		return fmt.Errorf("%w: %d slots needed, capacity is %d", ErrTooLarge, n, q.capacity)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrClosed
	}
	if q.depth+n > q.capacity {
		return fmt.Errorf("%w: %d slots needed, %d available", ErrFull, n, q.capacity-q.depth)
	}
	q.depth += n
	return nil
}

// Release gives back n reserved slots that will not be published.
func (q *Queue) Release(n int) {
	if n <= 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.depth = max(q.depth-n, 0)
}

// Publish enqueues a job into a slot claimed by Reserve. It never blocks:
// the reservation guarantees the buffer has room.
func (q *Queue) Publish(job Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrClosed
	}
	select {
	case q.jobs <- job:
		return nil
	default:
		// Unreachable while Reserve/Publish are paired correctly; returning
		// an error rather than blocking keeps a wiring bug from deadlocking
		// the request path.
		return fmt.Errorf("%w: no reserved slot available", ErrFull)
	}
}

// Jobs is the channel workers consume. It is closed by Close once no further
// jobs can be published, which is what lets workers drain and exit.
func (q *Queue) Jobs() <-chan Job { return q.jobs }

// Ack releases the slot held by a job a worker has taken off the queue.
// Workers call it on receipt, so Depth measures waiting work rather than
// work in progress; concurrency of in-progress work is bounded by the pool.
func (q *Queue) Ack() { q.Release(1) }

// Close stops admission and closes the job channel. It is idempotent.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}
	q.closed = true
	close(q.jobs)
}

// Closed reports whether the queue has been closed.
func (q *Queue) Closed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}
