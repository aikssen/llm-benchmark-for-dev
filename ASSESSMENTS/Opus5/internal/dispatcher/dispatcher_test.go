package dispatcher_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/dispatcher"
	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/platform/log"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/resilience"
)

// scriptedProvider returns a predetermined sequence of outcomes, so a test
// can describe exactly the failure pattern it wants to exercise.
type scriptedProvider struct {
	channel domain.Channel
	errs    []error
	calls   atomic.Int32
	delay   time.Duration
}

func (p *scriptedProvider) Name() string                 { return "scripted" }
func (p *scriptedProvider) Channel() domain.Channel      { return p.channel }
func (p *scriptedProvider) ValidateAddress(string) error { return nil }

func (p *scriptedProvider) Send(ctx context.Context, _ provider.SendRequest) (provider.SendResult, error) {
	n := int(p.calls.Add(1))

	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return provider.SendResult{}, provider.Transient(p.Name(), provider.CodeTimeout, "deadline exceeded")
		}
	}
	if n <= len(p.errs) {
		if err := p.errs[n-1]; err != nil {
			return provider.SendResult{}, err
		}
	}
	return provider.SendResult{MessageID: "msg-1"}, nil
}

func (p *scriptedProvider) Calls() int { return int(p.calls.Load()) }

func newDispatcher(t *testing.T, p provider.Provider, cfg dispatcher.Config) *dispatcher.Dispatcher {
	t.Helper()

	registry := provider.NewRegistry()
	registry.MustRegister(p)
	return dispatcher.New(registry, cfg, log.Discard(),
		// Instant backoff: the retry policy itself is tested in the
		// resilience package, here only its effect matters.
		dispatcher.WithRetrier(resilience.NewRetrier(cfg.Retry,
			resilience.WithSleep(func(context.Context, time.Duration) error { return nil }))),
	)
}

func request() dispatcher.Request {
	return dispatcher.Request{
		NotificationID: "ntf_1",
		DeliveryID:     "dlv_1",
		Channel:        domain.ChannelEmail,
		Recipient:      domain.Recipient{Address: "user@example.com"},
		Message:        domain.Message{Subject: "Hi", Body: "Hello"},
	}
}

func TestDispatchSucceedsOnTheFirstAttempt(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{channel: domain.ChannelEmail}
	d := newDispatcher(t, p, dispatcher.DefaultConfig())

	res, err := d.Dispatch(context.Background(), request())
	if err != nil {
		t.Fatalf("Dispatch() = %v, want nil", err)
	}
	if res.Attempts != 1 || res.MessageID != "msg-1" || res.Provider != "scripted" {
		t.Errorf("Result = %+v, want one attempt with the provider's message id", res)
	}
}

func TestDispatchRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		errs: []error{
			provider.Transient("scripted", provider.CodeUnavailable, "down"),
			provider.Transient("scripted", provider.CodeUnavailable, "still down"),
			nil, // recovers on the third attempt
		},
	}
	cfg := dispatcher.DefaultConfig()
	cfg.Retry.MaxAttempts = 3

	res, err := newDispatcher(t, p, cfg).Dispatch(context.Background(), request())
	if err != nil {
		t.Fatalf("Dispatch() = %v, want the delivery to recover", err)
	}
	if res.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", res.Attempts)
	}
	if p.Calls() != 3 {
		t.Errorf("provider was called %d times, want 3", p.Calls())
	}
}

func TestDispatchDoesNotRetryPermanentFailures(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		errs:    []error{provider.Permanent("scripted", provider.CodeRejected, "invalid recipient")},
	}
	cfg := dispatcher.DefaultConfig()
	cfg.Retry.MaxAttempts = 5

	res, err := newDispatcher(t, p, cfg).Dispatch(context.Background(), request())
	if err == nil {
		t.Fatal("Dispatch() = nil, want the permanent failure")
	}
	if res.Attempts != 1 || p.Calls() != 1 {
		t.Errorf("made %d calls (%d attempts), want exactly 1: a rejection will not change", p.Calls(), res.Attempts)
	}

	var perr *provider.Error
	if !errors.As(err, &perr) || perr.Code != provider.CodeRejected {
		t.Errorf("Dispatch() = %v, want the provider's rejection code to be preserved", err)
	}
}

func TestDispatchGivesUpAfterTheRetryBudget(t *testing.T) {
	t.Parallel()

	transient := provider.Transient("scripted", provider.CodeUnavailable, "down")
	p := &scriptedProvider{
		channel: domain.ChannelEmail,
		errs:    []error{transient, transient, transient, transient, transient},
	}
	cfg := dispatcher.DefaultConfig()
	cfg.Retry.MaxAttempts = 3
	// A high threshold keeps the breaker out of this test's way.
	cfg.Breaker.FailureThreshold = 100

	res, err := newDispatcher(t, p, cfg).Dispatch(context.Background(), request())
	if err == nil {
		t.Fatal("Dispatch() = nil, want the exhausted retry budget to surface")
	}
	if res.Attempts != 3 || p.Calls() != 3 {
		t.Errorf("made %d calls (%d attempts), want the configured budget of 3", p.Calls(), res.Attempts)
	}
}

// A slow provider must not pin a worker: the per-call timeout is what keeps
// one bad dependency from stalling every channel.
func TestDispatchTimesOutASlowProvider(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{channel: domain.ChannelEmail, delay: time.Hour}
	cfg := dispatcher.DefaultConfig()
	cfg.CallTimeout = 20 * time.Millisecond
	cfg.Retry.MaxAttempts = 1

	start := time.Now()
	_, err := newDispatcher(t, p, cfg).Dispatch(context.Background(), request())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Dispatch() = nil, want a timeout")
	}
	if elapsed > time.Second {
		t.Errorf("Dispatch() took %s, want it bounded by the call timeout", elapsed)
	}
}

func TestDispatchFailsFastWhenTheBreakerIsOpen(t *testing.T) {
	t.Parallel()

	transient := provider.Transient("scripted", provider.CodeUnavailable, "down")
	errs := make([]error, 50)
	for i := range errs {
		errs[i] = transient
	}
	p := &scriptedProvider{channel: domain.ChannelEmail, errs: errs}

	cfg := dispatcher.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 3
	cfg.Breaker.OpenDuration = time.Hour
	d := newDispatcher(t, p, cfg)

	for range 3 {
		_, _ = d.Dispatch(context.Background(), request())
	}
	if state, _ := d.BreakerState(domain.ChannelEmail); state != resilience.StateOpen {
		t.Fatalf("breaker state = %q, want %q", state, resilience.StateOpen)
	}

	callsBefore := p.Calls()
	_, err := d.Dispatch(context.Background(), request())
	if err == nil {
		t.Fatal("Dispatch() = nil, want a fast failure")
	}
	if p.Calls() != callsBefore {
		t.Error("the provider was called while its breaker was open")
	}

	// The delivery must carry an actionable reason rather than a raw
	// breaker error.
	var perr *provider.Error
	if !errors.As(err, &perr) || perr.Code != provider.CodeUnavailable {
		t.Errorf("Dispatch() = %v, want it reported as %q", err, provider.CodeUnavailable)
	}
	if perr.Retryable() {
		t.Error("failing fast on an open breaker must not be retried immediately")
	}
}

func TestDispatchReportsAnUnregisteredChannel(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{channel: domain.ChannelEmail}
	d := newDispatcher(t, p, dispatcher.DefaultConfig())

	req := request()
	req.Channel = domain.ChannelSMS

	_, err := d.Dispatch(context.Background(), req)
	var perr *provider.Error
	if !errors.As(err, &perr) {
		t.Fatalf("Dispatch() = %v, want a *provider.Error", err)
	}
	if perr.Retryable() {
		t.Error("a missing provider is not a transient condition")
	}
}

func TestBreakerStateIsReportedPerChannel(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{channel: domain.ChannelEmail}
	d := newDispatcher(t, p, dispatcher.DefaultConfig())

	if state, ok := d.BreakerState(domain.ChannelEmail); !ok || state != resilience.StateClosed {
		t.Errorf("BreakerState(email) = %q, %v; want closed, true", state, ok)
	}
	if _, ok := d.BreakerState(domain.ChannelPush); ok {
		t.Error("BreakerState() reported a breaker for an unregistered channel")
	}
}
