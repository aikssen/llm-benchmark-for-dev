package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
	"github.com/evercifuentes/notifications/internal/provider"
	"github.com/evercifuentes/notifications/internal/store"
	"github.com/evercifuentes/notifications/internal/validation"
	"github.com/evercifuentes/notifications/internal/worker"
)

func setupService(t *testing.T, failRate float64) (*Service, *store.MemoryStore, context.CancelFunc) {
	t.Helper()
	st := store.NewMemoryStore()
	prefs := validation.NewMemoryPreferences()
	limiter := validation.NewSlidingWindowLimiter(1000, time.Minute)
	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(2000),
		validation.NewPreferencesValidator(prefs),
		validation.NewRateLimitValidator(limiter),
	)
	registry := provider.NewRegistry()
	for _, ch := range []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush} {
		registry.Register(provider.NewSimulatedProvider(ch, failRate, 0, 0))
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool := worker.NewPool(st, pipeline, registry, worker.Config{
		Workers:    2,
		QueueSize:  64,
		MaxRetries: 2,
		BaseDelay:  time.Millisecond,
	})
	pool.Start(ctx)
	t.Cleanup(func() {
		cancel()
		pool.Stop()
	})

	return New(st, pool), st, cancel
}

func TestSubmitAndGet(t *testing.T) {
	svc, _, _ := setupService(t, 0)

	resp, err := svc.Submit(context.Background(), domain.CreateRequest{
		Recipients: []string{"user@example.com", "user@example.com", "other@example.com"},
		Channel:    domain.ChannelEmail,
		Subject:    "Hello",
		Body:       "World",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.NotificationIDs) != 2 {
		t.Fatalf("expected de-duplicated recipients, got %d", len(resp.NotificationIDs))
	}

	// Wait for async delivery.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := svc.Get(context.Background(), resp.NotificationIDs[0])
		if err != nil {
			t.Fatal(err)
		}
		if n.Status == domain.StatusDelivered {
			return
		}
		if n.Status == domain.StatusFailed || n.Status == domain.StatusRejected {
			t.Fatalf("unexpected terminal status: %s err=%s", n.Status, n.LastError)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for delivery")
}

func TestSubmitValidationRejected(t *testing.T) {
	svc, _, _ := setupService(t, 0)

	resp, err := svc.Submit(context.Background(), domain.CreateRequest{
		Recipients: []string{"not-an-email"},
		Channel:    domain.ChannelEmail,
		Subject:    "Hello",
		Body:       "World",
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := svc.Get(context.Background(), resp.NotificationIDs[0])
		if err != nil {
			t.Fatal(err)
		}
		if n.Status == domain.StatusRejected {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for rejection")
}

func TestSubmitInvalidRequest(t *testing.T) {
	svc, _, _ := setupService(t, 0)
	_, err := svc.Submit(context.Background(), domain.CreateRequest{
		Recipients: []string{"a@b.com"},
		Channel:    "fax",
		Body:       "hi",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
}

func TestList(t *testing.T) {
	svc, _, _ := setupService(t, 0)
	_, err := svc.Submit(context.Background(), domain.CreateRequest{
		Recipients: []string{"a@b.com"},
		Channel:    domain.ChannelEmail,
		Subject:    "s",
		Body:       "b",
	})
	if err != nil {
		t.Fatal(err)
	}

	items, total, err := svc.List(context.Background(), domain.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total < 1 || len(items) < 1 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}
}
