package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"notificationservice/internal/domain"
	"notificationservice/internal/provider"
	"notificationservice/internal/store"
	"notificationservice/internal/validation"
)

// scriptedProvider fails a configurable number of times before succeeding,
// and counts calls.
type scriptedProvider struct {
	calls    atomic.Int32
	failures int32 // number of initial calls that fail
	err      error // error returned by failing calls
}

func (s *scriptedProvider) Name() string            { return "scripted" }
func (s *scriptedProvider) Channel() domain.Channel { return domain.ChannelEmail }

func (s *scriptedProvider) Send(context.Context, domain.Notification, string) error {
	if s.calls.Add(1) <= s.failures {
		return s.err
	}
	return nil
}

type failingValidator struct{}

func (failingValidator) Name() string { return "always_fails" }
func (failingValidator) Validate(context.Context, validation.Input) error {
	return errors.New("nope")
}

func testConfig() Config {
	return Config{
		Workers:           2,
		QueueCapacity:     16,
		MaxAttempts:       3,
		RetryBaseDelay:    time.Millisecond,
		RetryMaxDelay:     5 * time.Millisecond,
		ValidationTimeout: time.Second,
		SendTimeout:       time.Second,
	}
}

// newTestPool wires a pool with a real memory store and the given provider
// and validators, starts it, and registers cleanup.
func newTestPool(t *testing.T, cfg Config, prov provider.Provider, validators ...validation.Validator) (*Pool, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	registry := provider.NewRegistry()
	if err := registry.Register(prov); err != nil {
		t.Fatal(err)
	}
	pool := New(cfg, st, registry, validation.NewPipeline(validators...), slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	t.Cleanup(func() {
		cancel()
		pool.Wait()
	})
	return pool, st
}

// submit stores a single-recipient notification and enqueues its delivery.
func submit(t *testing.T, pool *Pool, st store.Store) Job {
	t.Helper()
	n := domain.Notification{
		ID:      "ntf_1",
		Channel: domain.ChannelEmail,
		Message: domain.Message{Subject: "s", Body: "b"},
		Deliveries: []domain.Delivery{{
			ID:             "dlv_1",
			NotificationID: "ntf_1",
			Recipient:      "user@example.com",
			Status:         domain.DeliveryPending,
		}},
	}
	if err := st.CreateNotification(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	job := Job{NotificationID: "ntf_1", DeliveryID: "dlv_1"}
	if err := pool.EnqueueBatch([]Job{job}); err != nil {
		t.Fatal(err)
	}
	return job
}

// waitTerminal polls until the delivery reaches a terminal status.
func waitTerminal(t *testing.T, st store.Store, job Job) domain.Delivery {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("delivery did not reach a terminal status in time")
		case <-time.After(5 * time.Millisecond):
		}
		n, err := st.GetNotification(context.Background(), job.NotificationID)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range n.Deliveries {
			if d.ID == job.DeliveryID && (d.Status == domain.DeliveryDelivered || d.Status == domain.DeliveryFailed) {
				return d
			}
		}
	}
}

func TestProcessDeliversOnFirstAttempt(t *testing.T) {
	prov := &scriptedProvider{}
	pool, st := newTestPool(t, testConfig(), prov)

	d := waitTerminal(t, st, submit(t, pool, st))

	if d.Status != domain.DeliveryDelivered {
		t.Fatalf("status = %q (%s), want delivered", d.Status, d.LastError)
	}
	if d.Attempts != 1 || prov.calls.Load() != 1 {
		t.Errorf("attempts = %d, provider calls = %d, want 1/1", d.Attempts, prov.calls.Load())
	}
	if d.LastError != "" {
		t.Errorf("LastError = %q, want empty", d.LastError)
	}
}

func TestProcessRetriesTransientErrors(t *testing.T) {
	prov := &scriptedProvider{failures: 2, err: errors.New("throttled")}
	pool, st := newTestPool(t, testConfig(), prov)

	d := waitTerminal(t, st, submit(t, pool, st))

	if d.Status != domain.DeliveryDelivered {
		t.Fatalf("status = %q (%s), want delivered after retries", d.Status, d.LastError)
	}
	if d.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", d.Attempts)
	}
}

func TestProcessGivesUpAfterMaxAttempts(t *testing.T) {
	prov := &scriptedProvider{failures: 99, err: errors.New("still down")}
	pool, st := newTestPool(t, testConfig(), prov)

	d := waitTerminal(t, st, submit(t, pool, st))

	if d.Status != domain.DeliveryFailed {
		t.Fatalf("status = %q, want failed", d.Status)
	}
	if d.Attempts != 3 || prov.calls.Load() != 3 {
		t.Errorf("attempts = %d, provider calls = %d, want 3/3", d.Attempts, prov.calls.Load())
	}
	if d.LastError == "" {
		t.Error("LastError should describe the failure")
	}
}

func TestProcessDoesNotRetryPermanentErrors(t *testing.T) {
	prov := &scriptedProvider{failures: 99, err: provider.Permanent(errors.New("hard bounce"))}
	pool, st := newTestPool(t, testConfig(), prov)

	d := waitTerminal(t, st, submit(t, pool, st))

	if d.Status != domain.DeliveryFailed {
		t.Fatalf("status = %q, want failed", d.Status)
	}
	if prov.calls.Load() != 1 {
		t.Errorf("provider calls = %d, want exactly 1 (no retries on permanent errors)", prov.calls.Load())
	}
}

func TestValidationFailureSkipsProvider(t *testing.T) {
	prov := &scriptedProvider{}
	pool, st := newTestPool(t, testConfig(), prov, failingValidator{})

	d := waitTerminal(t, st, submit(t, pool, st))

	if d.Status != domain.DeliveryFailed {
		t.Fatalf("status = %q, want failed", d.Status)
	}
	if prov.calls.Load() != 0 {
		t.Errorf("provider was called %d times; validation failures must not reach the provider", prov.calls.Load())
	}
	if d.Attempts != 0 {
		t.Errorf("attempts = %d, want 0", d.Attempts)
	}
	if want := `validation "always_fails" failed`; !strings.Contains(d.LastError, want) {
		t.Errorf("LastError = %q, want it to contain %q", d.LastError, want)
	}
}

func TestEnqueueBatchBackpressure(t *testing.T) {
	cfg := testConfig()
	cfg.QueueCapacity = 2
	st := store.NewMemory()
	registry := provider.NewRegistry()
	if err := registry.Register(&scriptedProvider{}); err != nil {
		t.Fatal(err)
	}
	// Deliberately not started: jobs stay queued.
	pool := New(cfg, st, registry, validation.NewPipeline(), slog.New(slog.DiscardHandler))

	jobs := func(n int) []Job {
		out := make([]Job, n)
		for i := range out {
			out[i] = Job{NotificationID: fmt.Sprintf("ntf_%d", i), DeliveryID: fmt.Sprintf("dlv_%d", i)}
		}
		return out
	}

	// A batch larger than the whole queue is rejected atomically.
	if err := pool.EnqueueBatch(jobs(3)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	if len(pool.queue) != 0 {
		t.Fatalf("rejected batch must not partially enqueue, queue has %d jobs", len(pool.queue))
	}

	if err := pool.EnqueueBatch(jobs(2)); err != nil {
		t.Fatalf("batch within capacity should be accepted: %v", err)
	}
	// Queue is now full: even a single job is rejected.
	if err := pool.EnqueueBatch(jobs(1)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull on full queue, got %v", err)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	base, maxDelay := 100*time.Millisecond, time.Second
	prev := time.Duration(0)
	for attempt := 1; attempt <= 10; attempt++ {
		d := backoff(base, maxDelay, attempt)
		// Jitter is ±25%, so the hard bounds are [0.75*base, 1.25*max].
		if d < base*3/4 || d > maxDelay*5/4 {
			t.Errorf("attempt %d: backoff %v outside [%v, %v]", attempt, d, base*3/4, maxDelay*5/4)
		}
		if attempt <= 3 && d <= prev/2 {
			t.Errorf("attempt %d: backoff %v did not grow from %v", attempt, d, prev)
		}
		prev = d
	}
}
