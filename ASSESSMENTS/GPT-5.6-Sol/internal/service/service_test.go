package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"notification/internal/memory"
	"notification/internal/notification"
)

func TestSubmitIsIdempotent(t *testing.T) {
	t.Parallel()
	repository := memory.NewRepository()
	processor := NewProcessor(repository, fakeRegistry{}, nil, testRetryPolicy())
	svc := New(repository, processor, Config{Workers: 0, QueueCapacity: 2, MaxRecipients: 2, MaxBodyBytes: 100})
	defer stopService(t, svc)
	input := Submission{
		Recipients: []string{"user@example.com"}, Channel: notification.ChannelEmail,
		Message: notification.Message{Subject: "subject", Body: "body"}, IdempotencyKey: "key-1",
	}

	first, replayed, err := svc.Submit(context.Background(), input)
	if err != nil || replayed {
		t.Fatalf("first Submit() replayed %v, error %v", replayed, err)
	}
	second, replayed, err := svc.Submit(context.Background(), input)
	if err != nil || !replayed || second.ID != first.ID {
		t.Fatalf("second Submit() id %q, replayed %v, error %v", second.ID, replayed, err)
	}

	input.Message.Body = "different"
	if _, _, err = svc.Submit(context.Background(), input); !errors.Is(err, notification.ErrIdempotencyConflict) {
		t.Fatalf("conflicting Submit() error = %v", err)
	}
}

func TestSubmitRejectsWholeBatchWhenQueueCannotFit(t *testing.T) {
	t.Parallel()
	repository := memory.NewRepository()
	processor := NewProcessor(repository, fakeRegistry{}, nil, testRetryPolicy())
	svc := New(repository, processor, Config{Workers: 0, QueueCapacity: 2, MaxRecipients: 2, MaxBodyBytes: 100})
	defer stopService(t, svc)

	_, _, err := svc.Submit(context.Background(), Submission{
		Recipients: []string{"one@example.com", "two@example.com"}, Channel: notification.ChannelEmail,
		Message: notification.Message{Subject: "subject", Body: "body"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Submit(context.Background(), Submission{
		Recipients: []string{"three@example.com"}, Channel: notification.ChannelEmail,
		Message: notification.Message{Subject: "subject", Body: "body"},
	})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("second Submit() error = %v, want ErrQueueFull", err)
	}
	page, _ := svc.List(context.Background(), 20, 0)
	if page.Total != 1 {
		t.Fatalf("persisted notifications = %d, want 1", page.Total)
	}
}

func TestSubmitRejectsDuplicateRecipients(t *testing.T) {
	t.Parallel()
	repository := memory.NewRepository()
	processor := NewProcessor(repository, fakeRegistry{}, nil, testRetryPolicy())
	svc := New(repository, processor, Config{Workers: 0, QueueCapacity: 2, MaxRecipients: 2, MaxBodyBytes: 100})
	defer stopService(t, svc)
	_, _, err := svc.Submit(context.Background(), Submission{
		Recipients: []string{"same@example.com", "same@example.com"}, Channel: notification.ChannelEmail,
		Message: notification.Message{Subject: "subject", Body: "body"},
	})
	var invalid InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("Submit() error = %v, want InvalidInputError", err)
	}
}

func stopService(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
