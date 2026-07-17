package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"notifications/internal/domain"
)

// fakeProvider implements Provider with scripted behaviour.
type fakeProvider struct {
	channel domain.Channel
	calls   atomic.Int32
	// fn returns the error for the n-th call (1-based).
	fn func(call int32) error
}

func (f *fakeProvider) Channel() domain.Channel { return f.channel }

func (f *fakeProvider) Send(_ context.Context, _ *domain.Notification) error {
	n := f.calls.Add(1)
	return f.fn(n)
}

func testNotification() *domain.Notification {
	return domain.New("r", domain.ChannelEmail, "user@example.com", domain.Content{Body: "x"}, time.Now())
}

func fastResilient(inner Provider) *ResilientProvider {
	r := Wrap(inner, ResilientConfig{
		AttemptTimeout:   time.Second,
		MaxAttempts:      4,
		BaseDelay:        time.Millisecond,
		MaxDelay:         5 * time.Millisecond,
		BreakerThreshold: 10,
		BreakerResetWait: 50 * time.Millisecond,
	})
	r.sleep = func(context.Context, time.Duration) error { return nil } // no real backoff in tests
	return r
}

func TestResilientRetriesUntilSuccess(t *testing.T) {
	inner := &fakeProvider{channel: domain.ChannelEmail, fn: func(call int32) error {
		if call < 3 {
			return Temporaryf("intermittent %d", call)
		}
		return nil
	}}
	r := fastResilient(inner)
	n := testNotification()

	if err := r.Send(context.Background(), n); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if inner.calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", inner.calls.Load())
	}
	if n.Attempts != 3 {
		t.Fatalf("expected notification attempts=3, got %d", n.Attempts)
	}
}

func TestResilientDoesNotRetryPermanentErrors(t *testing.T) {
	inner := &fakeProvider{channel: domain.ChannelEmail, fn: func(int32) error {
		return Permanentf("unknown recipient")
	}}
	r := fastResilient(inner)

	err := r.Send(context.Background(), testNotification())
	if err == nil || IsTemporary(err) {
		t.Fatalf("expected permanent error, got %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("permanent errors must not be retried; got %d calls", inner.calls.Load())
	}
}

func TestResilientExhaustsAttempts(t *testing.T) {
	inner := &fakeProvider{channel: domain.ChannelEmail, fn: func(int32) error {
		return Temporaryf("still broken")
	}}
	r := fastResilient(inner)

	err := r.Send(context.Background(), testNotification())
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if inner.calls.Load() != 4 {
		t.Fatalf("expected 4 attempts, got %d", inner.calls.Load())
	}
}

// slowProvider respects context cancellation but takes delay to answer.
type slowProvider struct{ delay time.Duration }

func (s slowProvider) Channel() domain.Channel { return domain.ChannelEmail }

func (s slowProvider) Send(ctx context.Context, _ *domain.Notification) error {
	t := time.NewTimer(s.delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return Temporaryf("interrupted: %v", ctx.Err())
	case <-t.C:
		return nil
	}
}

func TestResilientHonoursAttemptTimeout(t *testing.T) {
	r := fastResilient(slowProvider{delay: 200 * time.Millisecond})
	r.timeout = 20 * time.Millisecond

	start := time.Now()
	err := r.Send(context.Background(), testNotification())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !IsTemporary(err) {
		t.Fatalf("timeouts should be retryable, got %v", err)
	}
	// 4 attempts * ~20ms must be well below the unbounded sleep.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("attempt timeout not enforced; took %s", elapsed)
	}
}

func TestCircuitBreakerOpensAndHalfOpens(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)

	for i := 0; i < 3; i++ {
		if err := cb.Allow(); err != nil {
			t.Fatalf("breaker should allow call %d", i)
		}
		cb.OnFailure()
	}
	// Threshold reached: calls are short-circuited.
	if err := cb.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// After the reset window, one probe is allowed (half-open).
	cb.now = func() time.Time { return time.Now().Add(2 * time.Second) }
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected probe to be allowed, got %v", err)
	}

	// Probe succeeds: breaker closes again.
	cb.OnSuccess()
	if err := cb.Allow(); err != nil {
		t.Fatalf("breaker should be closed after successful probe, got %v", err)
	}
}

func TestCircuitBreakerProbeFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, time.Second)
	cb.OnFailure() // opens immediately (threshold 1)

	if err := cb.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open, got %v", err)
	}
	cb.now = func() time.Time { return time.Now().Add(2 * time.Second) }
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected half-open probe, got %v", err)
	}
	cb.OnFailure() // probe failed -> open again

	if err := cb.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected breaker to re-open after failed probe, got %v", err)
	}
}

func TestResilientSkipsCallsWhileBreakerOpen(t *testing.T) {
	inner := &fakeProvider{channel: domain.ChannelEmail, fn: func(int32) error {
		return Temporaryf("down")
	}}
	r := fastResilient(inner)
	r.breaker = NewCircuitBreaker(2, time.Hour) // opens fast, stays open

	_ = r.Send(context.Background(), testNotification())
	_ = r.Send(context.Background(), testNotification())

	// The breaker opens after 2 real failures; subsequent sends are
	// short-circuited, so the inner provider sees far fewer than
	// 2 sends * 4 attempts calls.
	if got := inner.calls.Load(); got >= 8 {
		t.Fatalf("circuit breaker did not short-circuit calls; inner saw %d calls", got)
	}
}

func TestSimulatedDeterministicHooks(t *testing.T) {
	p := NewSimulated(SimulatedConfig{Channel: domain.ChannelEmail}) // zero latency, zero error rate

	// Permanent failure hook.
	err := p.Send(context.Background(), domain.New("r", domain.ChannelEmail, "undeliverable@example.com", domain.Content{Body: "x"}, time.Now()))
	if err == nil || IsTemporary(err) {
		t.Fatalf("expected permanent error for undeliverable recipient, got %v", err)
	}

	// Happy path.
	if err := p.Send(context.Background(), testNotification()); err != nil {
		t.Fatalf("expected success with errorRate=0, got %v", err)
	}
}

func TestSimulatedHonoursCancellation(t *testing.T) {
	p := NewSimulated(SimulatedConfig{
		Channel:    domain.ChannelEmail,
		MinLatency: time.Hour,
		MaxLatency: time.Hour,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.Send(ctx, testNotification())
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("provider ignored context cancellation; took %s", elapsed)
	}
}

func TestRegistry(t *testing.T) {
	r, err := NewRegistry(
		&fakeProvider{channel: domain.ChannelEmail, fn: func(int32) error { return nil }},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Supports(domain.ChannelEmail) {
		t.Fatal("registry should support email")
	}
	if r.Supports(domain.ChannelSMS) {
		t.Fatal("registry should not support sms")
	}
	if _, err := NewRegistry(
		&fakeProvider{channel: domain.ChannelEmail, fn: func(int32) error { return nil }},
		&fakeProvider{channel: domain.ChannelEmail, fn: func(int32) error { return nil }},
	); err == nil {
		t.Fatal("expected error for duplicate channel registration")
	}
}
