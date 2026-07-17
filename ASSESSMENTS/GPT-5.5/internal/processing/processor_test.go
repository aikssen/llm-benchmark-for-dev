package processing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/store"
	"notification-service/internal/validation"
)

func TestProcessShortCircuitsValidation(t *testing.T) {
	repository := store.NewMemoryStore()
	registry := provider.NewRegistry()
	registry.Register(domain.ChannelEmail, successSender{name: "success"})

	notification := testNotification("ntf_short_circuit", domain.ChannelEmail, "user@example.com")
	if err := repository.Save(context.Background(), notification); err != nil {
		t.Fatalf("save notification: %v", err)
	}

	firstCalls := 0
	secondCalls := 0
	processor := NewProcessor(repository, registry, []validation.Validator{
		validation.FuncValidator{
			ValidatorName: "first",
			Fn: func(context.Context, domain.Notification) error {
				firstCalls++
				return errors.New("boom")
			},
		},
		validation.FuncValidator{
			ValidatorName: "second",
			Fn: func(context.Context, domain.Notification) error {
				secondCalls++
				return nil
			},
		},
	}, Config{MaxAttempts: 1, SendTimeout: time.Second, RetryBackoff: time.Millisecond})

	processor.process(context.Background(), notification.ID)

	updated, err := repository.Get(context.Background(), notification.ID)
	if err != nil {
		t.Fatalf("get notification: %v", err)
	}
	if updated.Status != domain.StatusRejected {
		t.Fatalf("status = %q, want %q", updated.Status, domain.StatusRejected)
	}
	if firstCalls != 1 {
		t.Fatalf("first validator calls = %d, want 1", firstCalls)
	}
	if secondCalls != 0 {
		t.Fatalf("second validator calls = %d, want 0", secondCalls)
	}
	if !strings.Contains(updated.ValidationError, "first validation failed") {
		t.Fatalf("validation error = %q, want first validator context", updated.ValidationError)
	}
}

func TestProcessRetriesProviderAndDelivers(t *testing.T) {
	repository := store.NewMemoryStore()
	registry := provider.NewRegistry()
	sender := &flakySender{name: "flaky", failuresBeforeSuccess: 2}
	registry.Register(domain.ChannelEmail, sender)

	notification := testNotification("ntf_retry", domain.ChannelEmail, "user@example.com")
	if err := repository.Save(context.Background(), notification); err != nil {
		t.Fatalf("save notification: %v", err)
	}

	processor := NewProcessor(repository, registry, []validation.Validator{
		validation.FuncValidator{
			ValidatorName: "ok",
			Fn: func(context.Context, domain.Notification) error {
				return nil
			},
		},
	}, Config{MaxAttempts: 3, SendTimeout: time.Second, RetryBackoff: time.Millisecond})

	processor.process(context.Background(), notification.ID)

	updated, err := repository.Get(context.Background(), notification.ID)
	if err != nil {
		t.Fatalf("get notification: %v", err)
	}
	if updated.Status != domain.StatusDelivered {
		t.Fatalf("status = %q, want %q; failure=%q", updated.Status, domain.StatusDelivered, updated.FailureReason)
	}
	if updated.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", updated.Attempts)
	}
	if updated.Provider != "flaky" {
		t.Fatalf("provider = %q, want flaky", updated.Provider)
	}
	if updated.DeliveredAt == nil {
		t.Fatal("delivered_at is nil")
	}
}

func TestProcessMarksFailedAfterRetryExhaustion(t *testing.T) {
	repository := store.NewMemoryStore()
	registry := provider.NewRegistry()
	registry.Register(domain.ChannelEmail, &flakySender{name: "down", failuresBeforeSuccess: 99})

	notification := testNotification("ntf_failed", domain.ChannelEmail, "user@example.com")
	if err := repository.Save(context.Background(), notification); err != nil {
		t.Fatalf("save notification: %v", err)
	}

	processor := NewProcessor(repository, registry, nil, Config{MaxAttempts: 2, SendTimeout: time.Second, RetryBackoff: time.Millisecond})
	processor.process(context.Background(), notification.ID)

	updated, err := repository.Get(context.Background(), notification.ID)
	if err != nil {
		t.Fatalf("get notification: %v", err)
	}
	if updated.Status != domain.StatusFailed {
		t.Fatalf("status = %q, want %q", updated.Status, domain.StatusFailed)
	}
	if updated.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", updated.Attempts)
	}
	if updated.FailureReason == "" {
		t.Fatal("failure reason is empty")
	}
}

func TestEnqueueBatchRejectsOversizedBatch(t *testing.T) {
	repository := store.NewMemoryStore()
	registry := provider.NewRegistry()
	processor := NewProcessor(repository, registry, nil, Config{QueueSize: 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor.Start(ctx)
	defer processor.Stop()

	err := processor.EnqueueBatch(context.Background(), []string{"a", "b"})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("error = %v, want ErrQueueFull", err)
	}
}

func testNotification(id string, channel domain.Channel, recipient string) domain.Notification {
	now := time.Now().UTC()
	return domain.Notification{
		ID:        id,
		RequestID: "req_test",
		Recipient: recipient,
		Channel:   channel,
		Message: domain.Message{
			Subject: "Subject",
			Body:    "Body",
		},
		Status:    domain.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

type successSender struct {
	name string
}

func (s successSender) Name() string {
	return s.name
}

func (successSender) Send(context.Context, domain.Notification) error {
	return nil
}

type flakySender struct {
	name                  string
	failuresBeforeSuccess int
	calls                 int
}

func (s *flakySender) Name() string {
	return s.name
}

func (s *flakySender) Send(context.Context, domain.Notification) error {
	s.calls++
	if s.calls <= s.failuresBeforeSuccess {
		return errors.New("temporary failure")
	}
	return nil
}
