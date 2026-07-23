package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"notification-service/internal/domain"
)

func TestWorkerPool_SubmitAndDrain(t *testing.T) {
	var processed uint64

	handler := func(ctx context.Context, item *domain.SingleRecipientItem) error {
		atomic.AddUint64(&processed, 1)
		return nil
	}

	pool := NewPool(4, 100, handler)

	totalJobs := 50
	for i := 0; i < totalJobs; i++ {
		err := pool.Submit(&domain.SingleRecipientItem{
			ID:        "job-1",
			Recipient: "user@example.com",
		})
		if err != nil {
			t.Fatalf("failed to submit job: %v", err)
		}
	}

	pool.Stop()

	if atomic.LoadUint64(&processed) != uint64(totalJobs) {
		t.Fatalf("expected %d jobs processed, got %d", totalJobs, atomic.LoadUint64(&processed))
	}
}

func TestWorkerPool_QueueFull(t *testing.T) {
	blockChan := make(chan struct{})
	handler := func(ctx context.Context, item *domain.SingleRecipientItem) error {
		<-blockChan
		return nil
	}

	pool := NewPool(1, 1, handler) // 1 worker, capacity 1

	// Submit job 1 (worker picks it up)
	_ = pool.Submit(&domain.SingleRecipientItem{ID: "1"})

	// Wait briefly for worker to pick up job 1
	time.Sleep(10 * time.Millisecond)

	// Submit job 2 (fills queue channel capacity 1)
	_ = pool.Submit(&domain.SingleRecipientItem{ID: "2"})

	// Submit job 3 (queue full -> should return ErrQueueFull)
	err := pool.Submit(&domain.SingleRecipientItem{ID: "3"})
	if err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	close(blockChan)
	pool.Stop()
}
