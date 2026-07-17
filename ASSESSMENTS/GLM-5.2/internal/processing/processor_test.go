package processing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/notification-service/internal/channel"
	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/provider"
	"github.com/example/notification-service/internal/storage"
	"github.com/example/notification-service/internal/validation"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// deterministicProvider succeeds/fails based on the recipient address prefix:
// addresses whose "To" starts with "bad" always fail; all others succeed. The
// recipient validator requires valid email format, so tests use addresses like
// "ok@x.com" and "bad@x.com".
type deterministicProvider struct {
	name      string
	attempts  atomic.Int64
	failTimes int // per-call transient failures before success (0 = immediate)
}

func (d *deterministicProvider) Name() string { return d.name }
func (d *deterministicProvider) Send(ctx context.Context, out provider.Outbound) error {
	d.attempts.Add(1)
	if len(out.To) >= 3 && out.To[:3] == "bad" {
		return provider.ErrSimulatedTransient
	}
	return nil
}

func buildProcessor(t *testing.T, base provider.Provider) (*Processor, *storage.Memory, *channel.Registry) {
	t.Helper()
	repo := storage.NewMemory()
	pref := &alwaysAllow{}
	limiter := validation.NewSlidingWindowLimiter(time.Hour, 1000)
	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(),
		validation.NewPreferenceValidator(pref),
		validation.NewRateLimitValidator(limiter),
	)
	channels := channel.NewRegistry().Register(channel.NewEmail(base))
	queue := NewQueue(16)
	p := New(Config{Workers: 2, RecipientConcurrency: 4}, queue, repo, channels, pipeline, testLogger())
	return p, repo, channels
}

type alwaysAllow struct{}

func (alwaysAllow) Allows(context.Context, string, domain.Channel) (bool, error) { return true, nil }

func TestProcessorDeliversMultipleRecipients(t *testing.T) {
	// Wrap base provider with retry+attempts so transient failures can recover.
	base := &deterministicProvider{name: "email"}
	stack := provider.WithRetry(provider.RetryOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond})(
		provider.WithAttempts()(base),
	)
	p, repo, _ := buildProcessor(t, stack)

	n := &domain.Notification{
		ID:         "n1",
		Channel:    domain.ChannelEmail,
		Status:     domain.StatusAccepted,
		Recipients: []domain.Recipient{{ID: "u1", Email: "ok@x.com"}, {ID: "u2", Email: "ok@x.com"}},
		Message:    domain.Message{Subject: "S", Body: "B"},
	}
	if err := repo.Create(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if err := p.queue.Enqueue(n.ID); err != nil {
		t.Fatal(err)
	}

	p.Start(context.Background())
	waitForStatus(t, repo, "n1", []domain.Status{domain.StatusDelivered}, 2*time.Second)
	p.Stop()

	got, _ := repo.Get(context.Background(), "n1")
	if got.Status != domain.StatusDelivered {
		t.Fatalf("expected delivered, got %s", got.Status)
	}
	for _, r := range got.Results {
		if r.Status != domain.StatusDelivered {
			t.Fatalf("recipient %s not delivered: %s", r.RecipientID, r.Status)
		}
	}
}

func TestProcessorValidationFailure(t *testing.T) {
	base := &deterministicProvider{name: "email"}
	stack := provider.WithAttempts()(base)
	p, repo, _ := buildProcessor(t, stack)

	// u1 has invalid email (validation fails), u2 is valid.
	n := &domain.Notification{
		ID:         "n2",
		Channel:    domain.ChannelEmail,
		Status:     domain.StatusAccepted,
		Recipients: []domain.Recipient{{ID: "u1", Email: "not-an-email"}, {ID: "u2", Email: "ok@x.com"}},
		Message:    domain.Message{Subject: "S", Body: "B"},
	}
	_ = repo.Create(context.Background(), n)
	_ = p.queue.Enqueue(n.ID)

	p.Start(context.Background())
	waitForStatus(t, repo, "n2", []domain.Status{domain.StatusPartial}, 2*time.Second)
	p.Stop()

	got, _ := repo.Get(context.Background(), "n2")
	if got.Status != domain.StatusPartial {
		t.Fatalf("expected partial, got %s", got.Status)
	}
	res := got.ResultFor("u1")
	if res == nil || res.Status != domain.StatusValidationFailed {
		t.Fatalf("expected u1 validation_failed, got %+v", res)
	}
	if len(res.ValidationErrors) == 0 {
		t.Fatal("expected validation errors recorded")
	}
}

func TestProcessorDeliveryFailure(t *testing.T) {
	base := &deterministicProvider{name: "email"}
	stack := provider.WithRetry(provider.RetryOptions{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond})(
		provider.WithAttempts()(base),
	)
	p, repo, _ := buildProcessor(t, stack)

	n := &domain.Notification{
		ID:         "n3",
		Channel:    domain.ChannelEmail,
		Status:     domain.StatusAccepted,
		Recipients: []domain.Recipient{{ID: "u1", Email: "bad@x.com"}},
		Message:    domain.Message{Subject: "S", Body: "B"},
	}
	_ = repo.Create(context.Background(), n)
	_ = p.queue.Enqueue(n.ID)

	p.Start(context.Background())
	waitForStatus(t, repo, "n3", []domain.Status{domain.StatusFailed}, 2*time.Second)
	p.Stop()

	got, _ := repo.Get(context.Background(), "n3")
	res := got.ResultFor("u1")
	if res == nil || res.Status != domain.StatusFailed {
		t.Fatalf("expected failed, got %+v", res)
	}
	if res.Attempts != 2 {
		t.Fatalf("expected 2 attempts (retries exhausted), got %d", res.Attempts)
	}
}

func TestProcessorCircuitBreakerSkipsAfterFailures(t *testing.T) {
	base := &deterministicProvider{name: "email"}
	stack := provider.WithCircuitBreaker(provider.CircuitBreakerOptions{
		FailureThreshold: 2, Cooldown: time.Hour,
	})(
		provider.WithRetry(provider.RetryOptions{MaxAttempts: 1})(
			provider.WithAttempts()(base),
		),
	)
	p, repo, _ := buildProcessor(t, stack)

	// Three recipients all failing; after 2 failures the circuit opens and
	// the third should be skipped with ErrCircuitOpen (failed status).
	n := &domain.Notification{
		ID:      "n4",
		Channel: domain.ChannelEmail,
		Status:  domain.StatusAccepted,
		Recipients: []domain.Recipient{
			{ID: "u1", Email: "bad@x.com"},
			{ID: "u2", Email: "bad@x.com"},
			{ID: "u3", Email: "bad@x.com"},
		},
		Message: domain.Message{Subject: "S", Body: "B"},
	}
	_ = repo.Create(context.Background(), n)
	_ = p.queue.Enqueue(n.ID)

	p.Start(context.Background())
	waitForStatus(t, repo, "n4", []domain.Status{domain.StatusFailed}, 2*time.Second)
	p.Stop()

	got, _ := repo.Get(context.Background(), "n4")
	for _, r := range got.Results {
		if r.Status != domain.StatusFailed {
			t.Fatalf("expected all failed, got %s for %s", r.Status, r.RecipientID)
		}
	}
}

func TestQueueBackpressure(t *testing.T) {
	q := NewQueue(2)
	if err := q.Enqueue("a"); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("b"); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("c"); !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	// Drain one and enqueue again.
	id, ok := q.Dequeue(context.Background())
	if !ok || id != "a" {
		t.Fatalf("expected a, got %s ok=%v", id, ok)
	}
	if err := q.Enqueue("c"); err != nil {
		t.Fatalf("expected enqueue after drain to succeed, got %v", err)
	}
}

func TestQueueDequeueRespectsContext(t *testing.T) {
	q := NewQueue(1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, ok := q.Dequeue(ctx); ok {
		t.Fatal("expected dequeue to return false on cancelled ctx")
	}
}

// waitForStatus polls until the notification reaches one of the target
// terminal statuses or times out.
func waitForStatus(t *testing.T, repo *storage.Memory, id domain.ID, want []domain.Status, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := repo.Get(context.Background(), id)
		if err == nil && n.Status.IsTerminal() {
			for _, w := range want {
				if n.Status == w {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	n, _ := repo.Get(context.Background(), id)
	if n != nil {
		t.Fatalf("timed out waiting for status %v, got %s", want, n.Status)
	}
	t.Fatalf("timed out waiting for status %v", want)
}
