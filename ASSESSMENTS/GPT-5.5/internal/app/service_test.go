package app

import (
	"context"
	"errors"
	"testing"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/store"
)

func TestSubmitCreatesOneNotificationPerRecipientAndEnqueuesBatch(t *testing.T) {
	repository := store.NewMemoryStore()
	queue := &recordingBatchQueue{}
	registry := provider.NewRegistry()
	registry.Register(domain.ChannelEmail, noopSender{name: "email"})

	service := NewService(repository, queue, registry)
	result, err := service.Submit(context.Background(), SubmitCommand{
		Recipients: []string{"one@example.com", "two@example.com"},
		Channel:    domain.ChannelEmail,
		Message: domain.Message{
			Subject: "Hello",
			Body:    "Body",
		},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	if result.RequestID == "" {
		t.Fatal("request id is empty")
	}
	if len(result.Notifications) != 2 {
		t.Fatalf("notifications = %d, want 2", len(result.Notifications))
	}
	if len(queue.ids) != 2 {
		t.Fatalf("enqueued ids = %d, want 2", len(queue.ids))
	}

	list, err := repository.List(context.Background(), store.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("stored notifications = %d, want 2", len(list))
	}
	for _, notification := range list {
		if notification.Status != domain.StatusPending {
			t.Fatalf("status = %q, want pending", notification.Status)
		}
	}
}

func TestSubmitMarksNotificationsFailedWhenBatchQueueFails(t *testing.T) {
	repository := store.NewMemoryStore()
	queue := &recordingBatchQueue{err: errors.New("queue down")}
	registry := provider.NewRegistry()
	registry.Register(domain.ChannelEmail, noopSender{name: "email"})

	service := NewService(repository, queue, registry)
	_, err := service.Submit(context.Background(), SubmitCommand{
		Recipients: []string{"one@example.com", "two@example.com"},
		Channel:    domain.ChannelEmail,
		Message: domain.Message{
			Body: "Body",
		},
	})
	if err == nil {
		t.Fatal("submit succeeded, want error")
	}

	list, listErr := repository.List(context.Background(), store.ListFilter{Limit: 10})
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(list) != 2 {
		t.Fatalf("stored notifications = %d, want 2", len(list))
	}
	for _, notification := range list {
		if notification.Status != domain.StatusFailed {
			t.Fatalf("status = %q, want failed", notification.Status)
		}
		if notification.FailureReason == "" {
			t.Fatal("failure reason is empty")
		}
	}
}

func TestSubmitRejectsUnsupportedChannel(t *testing.T) {
	service := NewService(store.NewMemoryStore(), &recordingBatchQueue{}, provider.NewRegistry())
	_, err := service.Submit(context.Background(), SubmitCommand{
		Recipients: []string{"user@example.com"},
		Channel:    domain.ChannelEmail,
		Message: domain.Message{
			Body: "Body",
		},
	})
	if !errors.Is(err, ErrUnsupportedChannel) {
		t.Fatalf("error = %v, want ErrUnsupportedChannel", err)
	}
}

type recordingBatchQueue struct {
	ids []string
	err error
}

func (q *recordingBatchQueue) Enqueue(context.Context, string) error {
	panic("service should use EnqueueBatch when available")
}

func (q *recordingBatchQueue) EnqueueBatch(_ context.Context, ids []string) error {
	q.ids = append(q.ids, ids...)
	return q.err
}

type noopSender struct {
	name string
}

func (s noopSender) Name() string {
	return s.name
}

func (noopSender) Send(context.Context, domain.Notification) error {
	return nil
}
