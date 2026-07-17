package service_test

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/example/notification-orchestrator/internal/domain"
	"github.com/example/notification-orchestrator/internal/provider"
	"github.com/example/notification-orchestrator/internal/service"
	"github.com/example/notification-orchestrator/internal/store"
	"github.com/example/notification-orchestrator/internal/validation"
)

func TestServiceRetriesTransientProviderFailure(t *testing.T) {
	repository := store.NewMemoryRepository()
	sender := &scriptedProvider{failures: 2}
	dispatcher := newService(repository, sender)
	dispatcher.Start()
	t.Cleanup(func() { _ = dispatcher.Stop(context.Background()) })

	notification, created, err := dispatcher.Submit(service.CreateRequest{
		Recipients: []string{"person@example.com"}, Channel: domain.ChannelEmail, Message: "hello",
	}, "retry-key")
	if err != nil || !created {
		t.Fatalf("Submit() = (%v, %v, %v)", notification, created, err)
	}

	finished := eventually(t, repository, notification.ID)
	if finished.Status != domain.StatusDelivered || finished.Deliveries[0].Attempts != 3 {
		t.Fatalf("got status=%s attempts=%d, want delivered with 3 attempts", finished.Status, finished.Deliveries[0].Attempts)
	}
	if sender.CallCount() != 3 {
		t.Fatalf("provider calls = %d, want 3", sender.CallCount())
	}
}

func TestServiceAggregatesPartialFailure(t *testing.T) {
	repository := store.NewMemoryRepository()
	dispatcher := newService(repository, recipientProvider{})
	dispatcher.Start()
	t.Cleanup(func() { _ = dispatcher.Stop(context.Background()) })
	notification, _, err := dispatcher.Submit(service.CreateRequest{
		Recipients: []string{"good@example.com", "bad@example.com"}, Channel: domain.ChannelEmail, Message: "hello",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	finished := eventually(t, repository, notification.ID)
	if finished.Status != domain.StatusPartiallyFail {
		t.Fatalf("status = %s, want %s", finished.Status, domain.StatusPartiallyFail)
	}
}

func TestSubmitHonorsIdempotencyKey(t *testing.T) {
	repository := store.NewMemoryRepository()
	dispatcher := newService(repository, &scriptedProvider{})
	dispatcher.Start()
	t.Cleanup(func() { _ = dispatcher.Stop(context.Background()) })
	request := service.CreateRequest{Recipients: []string{"person@example.com"}, Channel: domain.ChannelEmail, Message: "hello"}
	first, firstCreated, err := dispatcher.Submit(request, "same-key")
	if err != nil || !firstCreated {
		t.Fatalf("first Submit() = (%v, %v, %v)", first, firstCreated, err)
	}
	second, secondCreated, err := dispatcher.Submit(request, "same-key")
	if err != nil || secondCreated || second.ID != first.ID {
		t.Fatalf("second Submit() = (%v, %v, %v)", second, secondCreated, err)
	}
}

func newService(repository *store.MemoryRepository, sender provider.Provider) *service.Service {
	return service.New(repository,
		provider.NewRegistry(map[domain.Channel]provider.Provider{domain.ChannelEmail: sender}),
		validation.NewPipeline(1, validation.RecipientFormat{}, validation.MessageContent{MaxBytes: 1000}),
		service.Config{QueueCapacity: 8, NotificationWorkers: 1, RecipientWorkers: 2, MaxRecipients: 10, MaxAttempts: 3, AttemptTimeout: time.Second, RetryBaseDelay: time.Millisecond},
		log.New(io.Discard, "", 0),
	)
}

func eventually(t *testing.T, repository *store.MemoryRepository, id string) *domain.Notification {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		notification, err := repository.Get(id)
		if err == nil && (notification.Status == domain.StatusDelivered || notification.Status == domain.StatusPartiallyFail || notification.Status == domain.StatusFailed) {
			return notification
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("notification %s did not finish", id)
	return nil
}

type scriptedProvider struct {
	mu       sync.Mutex
	failures int
	calls    int
}

func (p *scriptedProvider) Send(context.Context, provider.Payload) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.failures > 0 {
		p.failures--
		return errors.New("temporary provider outage")
	}
	return nil
}

func (p *scriptedProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type recipientProvider struct{}

func (recipientProvider) Send(_ context.Context, payload provider.Payload) error {
	if payload.Recipient == "bad@example.com" {
		return provider.Permanent(errors.New("recipient rejected"))
	}
	return nil
}
