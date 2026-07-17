package dispatcher

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"notifications/internal/domain"
	"notifications/internal/provider"
	"notifications/internal/store"
	"notifications/internal/validation"
)

// fakeProvider always succeeds.
type fakeProvider struct {
	channel domain.Channel
	calls   atomic.Int32
}

func (f *fakeProvider) Channel() domain.Channel { return f.channel }
func (f *fakeProvider) Send(_ context.Context, _ *domain.Notification) error {
	f.calls.Add(1)
	return nil
}

func testDispatcher(t *testing.T, workers, queueCap int) (*Dispatcher, *store.Memory, *fakeProvider) {
	t.Helper()
	st := store.NewMemory()
	fp := &fakeProvider{channel: domain.ChannelEmail}
	reg, err := provider.NewRegistry(provider.Wrap(fp, provider.ResilientConfig{
		AttemptTimeout:   time.Second,
		MaxAttempts:      2,
		BaseDelay:        time.Millisecond,
		MaxDelay:         5 * time.Millisecond,
		BreakerThreshold: 10,
		BreakerResetWait: 50 * time.Millisecond,
	}))
	if err != nil {
		t.Fatal(err)
	}
	pipe := validation.NewPipeline(validation.RecipientValidator{}, validation.ContentValidator{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(st, pipe, reg, workers, queueCap, logger)
	return d, st, fp
}

func waitStatus(t *testing.T, st *store.Memory, id string, want domain.Status) *domain.Notification {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := st.Get(context.Background(), id)
		if err == nil && n.Status == want {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
	n, _ := st.Get(context.Background(), id)
	t.Fatalf("notification %s did not reach status %s (current: %v)", id, want, n)
	return nil
}

func TestDeliverValidNotification(t *testing.T) {
	d, st, fp := testDispatcher(t, 2, 16)
	d.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Stop(ctx)
	}()

	n := domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Subject: "s", Body: "b"}, time.Now())
	_ = st.Upsert(context.Background(), n)
	if err := d.EnqueueBatch([]string{n.ID}); err != nil {
		t.Fatal(err)
	}

	got := waitStatus(t, st, n.ID, domain.StatusDelivered)
	if got.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", got.Attempts)
	}
	if fp.calls.Load() != 1 {
		t.Fatalf("provider should have been called once, got %d", fp.calls.Load())
	}
}

func TestRejectInvalidRecipientWithoutCallingProvider(t *testing.T) {
	d, st, fp := testDispatcher(t, 2, 16)
	d.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Stop(ctx)
	}()

	n := domain.New("r", domain.ChannelEmail, "not-an-email", domain.Content{Subject: "s", Body: "b"}, time.Now())
	_ = st.Upsert(context.Background(), n)
	if err := d.EnqueueBatch([]string{n.ID}); err != nil {
		t.Fatal(err)
	}

	got := waitStatus(t, st, n.ID, domain.StatusRejected)
	if got.LastError == "" {
		t.Fatal("rejected notification should carry a reason")
	}
	if fp.calls.Load() != 0 {
		t.Fatalf("rejected notification must never reach the provider; got %d calls", fp.calls.Load())
	}
}

func TestEnqueueBatchIsAllOrNothing(t *testing.T) {
	d, _, _ := testDispatcher(t, 1, 2)
	// Fill the queue capacity with a batch of 2.
	if err := d.EnqueueBatch([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	// One more ID does not fit: rejected entirely.
	if err := d.EnqueueBatch([]string{"c"}); err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	// A batch larger than remaining capacity is rejected entirely too.
	if err := d.EnqueueBatch([]string{"c", "d"}); err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull for oversized batch, got %v", err)
	}
}

func TestStopDrainsQueue(t *testing.T) {
	d, st, _ := testDispatcher(t, 2, 64)
	d.Start()

	ids := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		n := domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Subject: "s", Body: "b"}, time.Now())
		_ = st.Upsert(context.Background(), n)
		ids = append(ids, n.ID)
	}
	if err := d.EnqueueBatch(ids); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("stop should drain in time: %v", err)
	}

	for _, id := range ids {
		n, _ := st.Get(context.Background(), id)
		if !n.Status.Terminal() {
			t.Fatalf("notification %s not processed after drain: %s", id, n.Status)
		}
	}

	// Enqueueing after stop must fail.
	if err := d.EnqueueBatch([]string{"x"}); err != ErrStopped {
		t.Fatalf("expected ErrStopped, got %v", err)
	}
}

func TestUnknownChannelFails(t *testing.T) {
	d, st, _ := testDispatcher(t, 1, 8)
	d.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Stop(ctx)
	}()

	// Craft a notification for a channel with no registered provider
	// (only email is registered in this test). Validation passes for a
	// well-formed SMS, so the failure must surface at delivery time.
	n := domain.New("r", domain.ChannelSMS, "+15550000000", domain.Content{Body: "b"}, time.Now())
	_ = st.Upsert(context.Background(), n)
	_ = d.EnqueueBatch([]string{n.ID})

	got := waitStatus(t, st, n.ID, domain.StatusFailed)
	if got.LastError == "" {
		t.Fatal("failed notification should carry a reason")
	}
}
