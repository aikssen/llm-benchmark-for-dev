package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/service"
	"notification-service/internal/validation"
)

type counterProvider struct {
	name    string
	channel domain.Channel
	calls   int32
}

func (cp *counterProvider) Name() string { return cp.name }
func (cp *counterProvider) Channel() domain.Channel { return cp.channel }
func (cp *counterProvider) Send(ctx context.Context, n *domain.Notification) error {
	atomic.AddInt32(&cp.calls, 1)
	return nil
}

func TestDispatcher_Lifecycle(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryRepository()
	valEngine := validation.NewEngine()
	provRegistry := provider.NewRegistry()

	prov := &counterProvider{name: "CounterProv", channel: domain.ChannelEmail}
	provRegistry.Register(prov)

	orchestrator := service.NewOrchestrator(repo, valEngine, provRegistry)

	// Create dispatcher: 3 workers, queue size of 100
	dispatcher := NewDispatcher(3, 100, orchestrator)
	orchestrator.SetQueue(dispatcher)

	dispatcher.Start(ctx)

	// Enqueue 5 jobs
	for i := 1; i <= 5; i++ {
		req := &service.SubmitRequest{
			Recipients: []string{"test@example.com"},
			Channel:    domain.ChannelEmail,
			Content:    "Hello",
		}
		_, err := orchestrator.Submit(ctx, req)
		if err != nil {
			t.Fatalf("failed to submit: %v", err)
		}
	}

	// Give workers a brief moment to process
	time.Sleep(50 * time.Millisecond)

	// Call graceful stop
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := dispatcher.Stop(stopCtx)
	if err != nil {
		t.Fatalf("failed to stop gracefully: %v", err)
	}

	// Verify all 5 notifications were delivered
	calls := atomic.LoadInt32(&prov.calls)
	if calls != 5 {
		t.Errorf("expected 5 deliveries, got %d", calls)
	}

	// Verify all statuses in repo are Sent
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("failed to list notifications: %v", err)
	}

	for _, n := range list {
		if n.Status != domain.StatusSent {
			t.Errorf("expected notification %s status to be Sent, got %s", n.ID, n.Status)
		}
	}
}

func TestDispatcher_GracefulShutdownDrainsQueue(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryRepository()
	valEngine := validation.NewEngine()
	provRegistry := provider.NewRegistry()

	prov := &counterProvider{name: "SlowCounter", channel: domain.ChannelEmail}
	provRegistry.Register(prov)

	orchestrator := service.NewOrchestrator(repo, valEngine, provRegistry)

	// 1 worker, so we process jobs sequentially
	dispatcher := NewDispatcher(1, 100, orchestrator)
	orchestrator.SetQueue(dispatcher)

	dispatcher.Start(ctx)

	// Submit 10 jobs
	for i := 1; i <= 10; i++ {
		req := &service.SubmitRequest{
			Recipients: []string{"test@example.com"},
			Channel:    domain.ChannelEmail,
			Content:    "Hi",
		}
		_, _ = orchestrator.Submit(ctx, req)
	}

	// Immediately trigger graceful stop.
	// Graceful shutdown should close the queue channel and wait until the 1 worker drains all 10 jobs.
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := dispatcher.Stop(stopCtx)
	if err != nil {
		t.Fatalf("expected graceful stop without error, got: %v", err)
	}

	// Verify that indeed all 10 were processed even though Stop was called immediately
	calls := atomic.LoadInt32(&prov.calls)
	if calls != 10 {
		t.Errorf("expected all 10 notifications to be processed on graceful shutdown, got %d", calls)
	}
}
