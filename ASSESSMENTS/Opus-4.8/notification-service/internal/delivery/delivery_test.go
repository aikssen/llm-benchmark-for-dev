package delivery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
)

// scriptedProvider returns a preset sequence of errors, one per call, so we can
// drive the retry/circuit-breaker logic deterministically.
type scriptedProvider struct {
	channel domain.Channel
	results []error
	calls   atomic.Int32
	block   time.Duration // optional per-call delay to exercise timeouts
}

func (p *scriptedProvider) Channel() domain.Channel { return p.channel }

func (p *scriptedProvider) Send(ctx context.Context, _ *domain.Notification) error {
	n := int(p.calls.Add(1)) - 1
	if p.block > 0 {
		select {
		case <-time.After(p.block):
		case <-ctx.Done():
			return provider.Retryable(ctx.Err())
		}
	}
	if n < len(p.results) {
		return p.results[n]
	}
	return nil
}

func newDeliverer(t *testing.T, p provider.Provider, cfg Config) *Deliverer {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	return New(reg, cfg, nil)
}

func fastConfig() Config {
	return Config{
		Retry:          RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
		AttemptTimeout: 50 * time.Millisecond,
		CBMaxFailures:  100, // effectively disabled for retry-focused tests
		CBCooldown:     time.Second,
	}
}

func TestDeliver_SucceedsFirstTry(t *testing.T) {
	p := &scriptedProvider{channel: domain.ChannelEmail}
	d := newDeliverer(t, p, fastConfig())

	res := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if res.Err != nil {
		t.Fatalf("expected success, got %v", res.Err)
	}
	if res.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", res.Attempts)
	}
}

func TestDeliver_RetriesTransientThenSucceeds(t *testing.T) {
	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		results: []error{provider.Retryable(errors.New("temp")), provider.Retryable(errors.New("temp"))},
	}
	d := newDeliverer(t, p, fastConfig())

	res := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if res.Err != nil {
		t.Fatalf("expected eventual success, got %v", res.Err)
	}
	if res.Attempts != 3 {
		t.Errorf("expected 3 attempts (2 failures + success), got %d", res.Attempts)
	}
}

func TestDeliver_StopsOnPermanentError(t *testing.T) {
	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		results: []error{provider.Permanent(errors.New("rejected"))},
	}
	d := newDeliverer(t, p, fastConfig())

	res := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if res.Err == nil {
		t.Fatal("expected permanent failure")
	}
	if res.Attempts != 1 {
		t.Errorf("permanent error should not be retried; got %d attempts", res.Attempts)
	}
}

func TestDeliver_ExhaustsRetries(t *testing.T) {
	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		results: []error{
			provider.Retryable(errors.New("t")),
			provider.Retryable(errors.New("t")),
			provider.Retryable(errors.New("t")),
			provider.Retryable(errors.New("t")),
		},
	}
	d := newDeliverer(t, p, fastConfig())

	res := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if res.Err == nil {
		t.Fatal("expected exhaustion error")
	}
	if res.Attempts != 4 {
		t.Errorf("expected 4 attempts, got %d", res.Attempts)
	}
}

func TestDeliver_PerAttemptTimeout(t *testing.T) {
	// Provider blocks longer than the attempt timeout on every call.
	p := &scriptedProvider{channel: domain.ChannelEmail, block: 100 * time.Millisecond}
	cfg := fastConfig()
	cfg.AttemptTimeout = 10 * time.Millisecond
	cfg.Retry.MaxAttempts = 2
	d := newDeliverer(t, p, cfg)

	start := time.Now()
	res := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if res.Err == nil {
		t.Fatal("expected timeout-driven failure")
	}
	// Should not have waited for the full provider block on either attempt.
	if time.Since(start) > 80*time.Millisecond {
		t.Errorf("attempt timeout not enforced; took %v", time.Since(start))
	}
}

func TestDeliver_NoProvider(t *testing.T) {
	d := New(provider.NewRegistry(), fastConfig(), nil)
	res := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelSMS})
	if !errors.Is(res.Err, domain.ErrNoProvider) {
		t.Fatalf("expected ErrNoProvider, got %v", res.Err)
	}
}

func TestDeliver_CircuitOpensAndFailsFast(t *testing.T) {
	// Always-failing provider with a breaker that trips after 2 failures.
	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		results: []error{
			provider.Retryable(errors.New("t")), provider.Retryable(errors.New("t")),
			provider.Retryable(errors.New("t")), provider.Retryable(errors.New("t")),
		},
	}
	cfg := fastConfig()
	cfg.CBMaxFailures = 2
	cfg.CBCooldown = time.Minute
	cfg.Retry.MaxAttempts = 4
	d := newDeliverer(t, p, cfg)

	// First delivery trips the breaker partway through its retries.
	first := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if first.Err == nil {
		t.Fatal("expected failure")
	}
	callsAfterFirst := p.calls.Load()

	// A subsequent delivery should fail fast via the open breaker, making no new
	// provider calls.
	second := d.Deliver(context.Background(), &domain.Notification{Channel: domain.ChannelEmail})
	if !errors.Is(second.Err, ErrCircuitOpen) {
		t.Fatalf("expected circuit-open error, got %v", second.Err)
	}
	if p.calls.Load() != callsAfterFirst {
		t.Errorf("open breaker should make no provider calls; before=%d after=%d", callsAfterFirst, p.calls.Load())
	}
}

func TestDeliver_ContextCancelled(t *testing.T) {
	p := &scriptedProvider{channel: domain.ChannelEmail, results: []error{provider.Retryable(errors.New("t"))}}
	d := newDeliverer(t, p, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	res := d.Deliver(ctx, &domain.Notification{Channel: domain.ChannelEmail})
	if res.Err == nil {
		t.Fatal("expected cancellation error")
	}
	if res.Attempts != 0 {
		t.Errorf("expected 0 attempts on pre-cancelled context, got %d", res.Attempts)
	}
}
