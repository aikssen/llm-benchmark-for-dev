package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_ProcessesAllJobs(t *testing.T) {
	var processed atomic.Int64
	var seen sync.Map

	pool := New(4, 16, func(_ context.Context, id string) {
		seen.Store(id, true)
		processed.Add(1)
	}, nil)
	pool.Start(context.Background())

	const n = 100
	for i := range n {
		if err := pool.Submit(context.Background(), string(rune('A')+rune(i%26))+itoa(i)); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	// Drain via shutdown, which waits for all buffered/in-flight jobs.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if processed.Load() != n {
		t.Errorf("expected %d processed, got %d", n, processed.Load())
	}
}

func TestPool_SubmitAfterShutdownReturnsError(t *testing.T) {
	pool := New(1, 1, func(context.Context, string) {}, nil)
	pool.Start(context.Background())

	ctx := context.Background()
	_ = pool.Shutdown(ctx)

	if err := pool.Submit(ctx, "x"); !errors.Is(err, ErrPoolClosed) {
		t.Errorf("expected ErrPoolClosed, got %v", err)
	}
}

func TestPool_ShutdownDrainsBufferedJobs(t *testing.T) {
	var processed atomic.Int64
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	// Single worker that blocks on the first job until released, so subsequent
	// jobs pile up in the buffer and must be drained on shutdown.
	pool := New(1, 10, func(_ context.Context, _ string) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		processed.Add(1)
	}, nil)
	pool.Start(context.Background())

	for i := range 5 {
		if err := pool.Submit(context.Background(), itoa(i)); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	<-started // ensure the worker has picked up the first job
	close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if processed.Load() != 5 {
		t.Errorf("expected all 5 buffered jobs drained, got %d", processed.Load())
	}
}

func TestPool_SubmitRespectsContextCancellation(t *testing.T) {
	// Queue size 1, worker blocks, so the second submit cannot enqueue and must
	// honour its context deadline instead of blocking forever.
	block := make(chan struct{})
	pool := New(1, 1, func(context.Context, string) { <-block }, nil)
	pool.Start(context.Background())
	defer func() { close(block); _ = pool.Shutdown(context.Background()) }()

	// Fill the single worker and the single queue slot.
	_ = pool.Submit(context.Background(), "1")
	// The worker may or may not have taken job 1 yet; fill the buffer.
	_ = pool.Submit(context.Background(), "2")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := pool.Submit(ctx, "3")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded when queue is full, got %v", err)
	}
}

// itoa is a tiny helper to avoid importing strconv in tests.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
