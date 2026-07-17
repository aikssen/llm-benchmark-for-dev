// Package processing orchestrates asynchronous notification delivery.
//
// The Queue provides bounded buffering with backpressure: when full, enqueue
// fails fast so the API can reject bursts with 503 instead of consuming
// unbounded memory. A fixed-size pool of workers drains the queue, keeping
// resource usage predictable. Within a notification, recipients are processed
// concurrently up to a configurable limit; one recipient's failure never
// affects another.
package processing

import (
	"context"

	"github.com/example/notification-service/internal/domain"
)

// Queue is a bounded, in-memory FIFO of notification IDs.
type Queue struct {
	ch chan domain.ID
}

// NewQueue builds a queue with the given capacity.
func NewQueue(capacity int) *Queue {
	return &Queue{ch: make(chan domain.ID, capacity)}
}

// Enqueue adds an id. It never blocks: if the queue is full it returns
// domain.ErrQueueFull immediately (backpressure).
func (q *Queue) Enqueue(id domain.ID) error {
	select {
	case q.ch <- id:
		return nil
	default:
		return domain.ErrQueueFull
	}
}

// Dequeue returns the next id, or false when the queue is closed/ctx done.
func (q *Queue) Dequeue(ctx context.Context) (domain.ID, bool) {
	select {
	case id, ok := <-q.ch:
		return id, ok
	case <-ctx.Done():
		return "", false
	}
}

// Close stops the queue; in-flight Dequeuers drain remaining items then exit.
func (q *Queue) Close() { close(q.ch) }
