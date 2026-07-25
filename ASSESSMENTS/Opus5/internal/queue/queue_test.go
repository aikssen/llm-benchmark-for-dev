package queue_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/queue"
)

func job(id string) queue.Job {
	return queue.Job{NotificationID: "ntf_1", DeliveryID: id}
}

func TestReserveIsAllOrNothing(t *testing.T) {
	t.Parallel()

	q := queue.New(10)

	if err := q.Reserve(6); err != nil {
		t.Fatalf("Reserve(6) = %v, want nil", err)
	}
	// Only 4 slots remain, so a request for 5 must be refused entirely
	// rather than partially admitted.
	if err := q.Reserve(5); !errors.Is(err, queue.ErrFull) {
		t.Fatalf("Reserve(5) = %v, want ErrFull", err)
	}
	if got := q.Depth(); got != 6 {
		t.Errorf("Depth() = %d after the refused reservation, want 6", got)
	}
	if err := q.Reserve(4); err != nil {
		t.Fatalf("Reserve(4) = %v, want nil: the remaining slots should still be usable", err)
	}
}

func TestReserveRejectsRequestsLargerThanTheQueue(t *testing.T) {
	t.Parallel()

	q := queue.New(10)
	// Distinguished from ErrFull: this request can never succeed, so the
	// caller must be told to change the request, not to retry.
	if err := q.Reserve(11); !errors.Is(err, queue.ErrTooLarge) {
		t.Fatalf("Reserve(11) on a queue of 10 = %v, want ErrTooLarge", err)
	}
}

func TestPublishNeverBlocksWithinAReservation(t *testing.T) {
	t.Parallel()

	q := queue.New(3)
	if err := q.Reserve(3); err != nil {
		t.Fatalf("Reserve(3) = %v", err)
	}

	// All three publishes must complete with no consumer running: the
	// reservation guarantees the buffer has room, so the request path never
	// blocks on a busy worker pool.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 3 {
			if err := q.Publish(job(string(rune('a' + i)))); err != nil {
				t.Errorf("Publish() = %v, want nil", err)
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish() blocked despite a valid reservation")
	}
}

func TestAckFreesCapacity(t *testing.T) {
	t.Parallel()

	q := queue.New(2)
	if err := q.Reserve(2); err != nil {
		t.Fatalf("Reserve(2) = %v", err)
	}
	if err := q.Reserve(1); !errors.Is(err, queue.ErrFull) {
		t.Fatalf("Reserve(1) on a full queue = %v, want ErrFull", err)
	}

	_ = q.Publish(job("a"))
	<-q.Jobs()
	q.Ack()

	if got := q.Depth(); got != 1 {
		t.Errorf("Depth() = %d after one ack, want 1", got)
	}
	if err := q.Reserve(1); err != nil {
		t.Errorf("Reserve(1) after an ack = %v, want nil", err)
	}
}

func TestReleaseReturnsUnusedSlots(t *testing.T) {
	t.Parallel()

	q := queue.New(5)
	if err := q.Reserve(5); err != nil {
		t.Fatalf("Reserve(5) = %v", err)
	}
	q.Release(5)

	if got := q.Depth(); got != 0 {
		t.Errorf("Depth() = %d after releasing everything, want 0", got)
	}
	if err := q.Reserve(5); err != nil {
		t.Errorf("Reserve(5) after release = %v, want nil", err)
	}
}

func TestReleaseNeverDrivesDepthNegative(t *testing.T) {
	t.Parallel()

	q := queue.New(5)
	q.Release(10)
	if got := q.Depth(); got != 0 {
		t.Errorf("Depth() = %d, want 0", got)
	}
}

func TestClosedQueueRefusesNewWork(t *testing.T) {
	t.Parallel()

	q := queue.New(5)
	q.Close()

	if err := q.Reserve(1); !errors.Is(err, queue.ErrClosed) {
		t.Errorf("Reserve() after Close = %v, want ErrClosed", err)
	}
	if err := q.Publish(job("a")); !errors.Is(err, queue.ErrClosed) {
		t.Errorf("Publish() after Close = %v, want ErrClosed", err)
	}
	if !q.Closed() {
		t.Error("Closed() = false after Close()")
	}
}

func TestCloseIsIdempotentAndUnblocksConsumers(t *testing.T) {
	t.Parallel()

	q := queue.New(5)
	drained := make(chan int)
	go func() {
		count := 0
		for range q.Jobs() {
			q.Ack()
			count++
		}
		drained <- count
	}()

	if err := q.Reserve(2); err != nil {
		t.Fatalf("Reserve(2) = %v", err)
	}
	_ = q.Publish(job("a"))
	_ = q.Publish(job("b"))

	q.Close()
	q.Close() // must not panic

	if got := <-drained; got != 2 {
		t.Errorf("consumer drained %d jobs, want 2", got)
	}
}

// Concurrent admission must never exceed the bound: that guarantee is what
// makes memory usage predictable during a burst.
func TestConcurrentReservationsRespectTheBound(t *testing.T) {
	t.Parallel()

	const capacity = 64
	q := queue.New(capacity)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
	)
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := q.Reserve(4); err == nil {
				mu.Lock()
				granted += 4
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if granted != capacity {
		t.Errorf("granted %d slots, want exactly the capacity of %d", granted, capacity)
	}
	if got := q.Depth(); got != capacity {
		t.Errorf("Depth() = %d, want %d", got, capacity)
	}
}

func TestNewClampsCapacityToAtLeastOne(t *testing.T) {
	t.Parallel()

	q := queue.New(0)
	if got := q.Capacity(); got != 1 {
		t.Errorf("Capacity() = %d, want 1", got)
	}
	if err := q.Reserve(1); err != nil {
		t.Errorf("Reserve(1) = %v, want nil", err)
	}
}
