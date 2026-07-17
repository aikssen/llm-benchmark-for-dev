package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/validation"
)

type mockQueue struct {
	mu       sync.Mutex
	enqueued []string
}

func (mq *mockQueue) Enqueue(ctx context.Context, id string) error {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	mq.enqueued = append(mq.enqueued, id)
	return nil
}

type trackProvider struct {
	name        string
	channel     domain.Channel
	calls       int
	shouldFail  bool
	failUntil   int
	baseLatency time.Duration
}

func (tp *trackProvider) Name() string { return tp.name }
func (tp *trackProvider) Channel() domain.Channel { return tp.channel }
func (tp *trackProvider) Send(ctx context.Context, n *domain.Notification) error {
	tp.calls++
	if tp.shouldFail {
		return errors.New("simulated error")
	}
	if tp.calls <= tp.failUntil {
		return errors.New("simulated transient error")
	}
	return nil
}

func TestOrchestrator_Submit(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryRepository()
	valEngine := validation.NewEngine() // Empty validators (always passes)
	provRegistry := provider.NewRegistry()

	orchestrator := NewOrchestrator(repo, valEngine, provRegistry)
	mq := &mockQueue{}
	orchestrator.SetQueue(mq)

	// 1. Submit valid multi-recipient request
	req := &SubmitRequest{
		Recipients: []string{"user1@example.com", "user2@example.com"},
		Channel:    domain.ChannelEmail,
		Content:    "Hello Test",
	}

	resp, err := orchestrator.Submit(ctx, req)
	if err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	if resp.RequestID == "" {
		t.Error("expected non-empty RequestID")
	}

	if len(resp.NotificationIDs) != 2 {
		t.Errorf("expected 2 notification IDs, got %d", len(resp.NotificationIDs))
	}

	// Verify they are saved in repo and queued
	for _, id := range resp.NotificationIDs {
		n, err := repo.Get(ctx, id)
		if err != nil {
			t.Errorf("failed to retrieve saved notification %s: %v", id, err)
		}
		if n.Status != domain.StatusPending {
			t.Errorf("expected status to be Pending, got %s", n.Status)
		}
		if n.RequestID != resp.RequestID {
			t.Errorf("expected RequestID %s, got %s", resp.RequestID, n.RequestID)
		}
	}

	mq.mu.Lock()
	queuedCount := len(mq.enqueued)
	mq.mu.Unlock()

	if queuedCount != 2 {
		t.Errorf("expected 2 notifications enqueued, got %d", queuedCount)
	}

	// 2. Validate field checks
	badReqs := []struct {
		name string
		req  *SubmitRequest
	}{
		{"Empty recipients", &SubmitRequest{Recipients: []string{}, Channel: domain.ChannelEmail, Content: "hi"}},
		{"Empty content", &SubmitRequest{Recipients: []string{"test@test.com"}, Channel: domain.ChannelEmail, Content: ""}},
		{"Invalid channel", &SubmitRequest{Recipients: []string{"test@test.com"}, Channel: domain.Channel("slack"), Content: "hi"}},
	}

	for _, tc := range badReqs {
		_, err := orchestrator.Submit(ctx, tc.req)
		if err == nil {
			t.Errorf("expected submit failure for %s", tc.name)
		}
	}
}

func TestOrchestrator_ProcessNotification_Success(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryRepository()
	valEngine := validation.NewEngine()

	provRegistry := provider.NewRegistry()
	p1 := &trackProvider{name: "P1", channel: domain.ChannelEmail}
	provRegistry.Register(p1)

	orchestrator := NewOrchestrator(repo, valEngine, provRegistry)

	// Create pending notification in repo
	n := &domain.Notification{
		ID:        "notify-id",
		Recipient: "user@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Hello",
		Status:    domain.StatusPending,
	}
	_ = repo.Save(ctx, n)

	err := orchestrator.ProcessNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("ProcessNotification failed: %v", err)
	}

	// Verify state updates in repo
	updated, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}

	if updated.Status != domain.StatusSent {
		t.Errorf("expected status Sent, got %s", updated.Status)
	}

	if p1.calls != 1 {
		t.Errorf("expected provider 1 call, got %d", p1.calls)
	}
}

func TestOrchestrator_ProcessNotification_ValidationFailure(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryRepository()

	// Inject a validator that fails
	valEngine := validation.NewEngine(&validation.ContentValidator{})
	provRegistry := provider.NewRegistry()

	orchestrator := NewOrchestrator(repo, valEngine, provRegistry)

	// SMS with content exceeding 160 characters (invalid)
	n := &domain.Notification{
		ID:        "notify-id",
		Recipient: "+15550000000",
		Channel:   domain.ChannelSMS,
		Content:   strings.Repeat("a", 161),
		Status:    domain.StatusPending,
	}
	_ = repo.Save(ctx, n)

	err := orchestrator.ProcessNotification(ctx, n.ID)
	if err == nil {
		t.Fatal("expected process error because validation failed")
	}

	updated, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}

	if updated.Status != domain.StatusFailedValidation {
		t.Errorf("expected status FailedValidation, got %s", updated.Status)
	}

	if !strings.Contains(updated.ErrorReason, "exceeds limit") {
		t.Errorf("expected error reason to detail content limit failure, got %q", updated.ErrorReason)
	}
}

func TestOrchestrator_ProcessNotification_RetriesAndFallback(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryRepository()
	valEngine := validation.NewEngine()
	provRegistry := provider.NewRegistry()

	// Primary provider: fails 3 times, succeeds on 4th (meaning it fails all its 3 retry attempts)
	pPrimary := &trackProvider{name: "Primary", channel: domain.ChannelSMS, failUntil: 3}
	// Fallback provider: succeeds on 1st attempt
	pFallback := &trackProvider{name: "Fallback", channel: domain.ChannelSMS, failUntil: 0}

	provRegistry.Register(pPrimary)
	provRegistry.Register(pFallback)

	orchestrator := NewOrchestrator(repo, valEngine, provRegistry)

	n := &domain.Notification{
		ID:        "sms-notify",
		Recipient: "+15551234567",
		Channel:   domain.ChannelSMS,
		Content:   "Hi",
		Status:    domain.StatusPending,
	}
	_ = repo.Save(ctx, n)

	// Process notification (will run retries on Primary, then failover to Fallback)
	err := orchestrator.ProcessNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("expected successful failover, got error: %v", err)
	}

	updated, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}

	if updated.Status != domain.StatusSent {
		t.Errorf("expected status Sent, got %s", updated.Status)
	}

	// Primary should be called exactly 3 times (initial + 2 retries)
	if pPrimary.calls != 3 {
		t.Errorf("expected primary provider to be tried 3 times, got %d", pPrimary.calls)
	}

	// Fallback should be called exactly 1 time
	if pFallback.calls != 1 {
		t.Errorf("expected fallback provider to be tried 1 time, got %d", pFallback.calls)
	}

	// Final retry count tracker in DB should reflect the attempts on the provider that succeeded (starts at 0)
	if updated.RetryCount != 0 {
		t.Errorf("expected RetryCount to be 0 (first attempt on fallback succeeded), got %d", updated.RetryCount)
	}
}
