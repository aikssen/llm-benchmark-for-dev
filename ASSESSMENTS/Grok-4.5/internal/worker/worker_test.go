package worker

import (
	"context"
	"testing"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
	"github.com/evercifuentes/notifications/internal/provider"
	"github.com/evercifuentes/notifications/internal/store"
	"github.com/evercifuentes/notifications/internal/validation"
)

type tempProvider struct {
	channel domain.Channel
	fails   int
	calls   int
}

func (p *tempProvider) Channel() domain.Channel { return p.channel }

func (p *tempProvider) Send(context.Context, *domain.Notification) error {
	p.calls++
	if p.calls <= p.fails {
		return provider.ErrTemporary
	}
	return nil
}

func TestWorkerRetriesAndDelivers(t *testing.T) {
	st := store.NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := &domain.Notification{
		ID:        "n1",
		Recipient: "a@b.com",
		Channel:   domain.ChannelEmail,
		Subject:   "s",
		Body:      "b",
		Status:    domain.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.Create(ctx, n); err != nil {
		t.Fatal(err)
	}

	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(2000),
	)
	registry := provider.NewRegistry()
	tp := &tempProvider{channel: domain.ChannelEmail, fails: 2}
	registry.Register(tp)

	pool := NewPool(st, pipeline, registry, Config{
		Workers:    1,
		QueueSize:  8,
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
	})
	pool.Start(ctx)
	defer func() {
		cancel()
		pool.Stop()
	}()

	if ok := pool.Enqueue(ctx, Job{NotificationID: "n1"}); !ok {
		t.Fatal("enqueue failed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := st.Get(ctx, "n1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == domain.StatusDelivered {
			if got.Attempts != 3 {
				t.Fatalf("attempts=%d", got.Attempts)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for delivery after retries")
}

func TestWorkerRejectsInvalid(t *testing.T) {
	st := store.NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := &domain.Notification{
		ID:        "n2",
		Recipient: "bad",
		Channel:   domain.ChannelEmail,
		Subject:   "s",
		Body:      "b",
		Status:    domain.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_ = st.Create(ctx, n)

	pipeline := validation.NewPipeline(validation.NewRecipientValidator())
	registry := provider.NewRegistry()
	registry.Register(provider.NewSimulatedProvider(domain.ChannelEmail, 0, 0, 0))

	pool := NewPool(st, pipeline, registry, Config{
		Workers:   1,
		QueueSize: 4,
	})
	pool.Start(ctx)
	defer func() {
		cancel()
		pool.Stop()
	}()

	_ = pool.Enqueue(ctx, Job{NotificationID: "n2"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := st.Get(ctx, "n2")
		if got.Status == domain.StatusRejected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected rejected status")
}
