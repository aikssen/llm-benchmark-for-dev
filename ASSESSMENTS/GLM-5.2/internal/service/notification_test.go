package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/processing"
	"github.com/example/notification-service/internal/storage"
)

func TestServiceCreateAndEnqueue(t *testing.T) {
	repo := storage.NewMemory()
	queue := processing.NewQueue(2)
	svc := New(repo, queue, []domain.Channel{domain.ChannelEmail})

	n, err := svc.Create(context.Background(), domain.ChannelEmail,
		[]domain.Recipient{{ID: "u1", Email: "a@b.com"}},
		domain.Message{Subject: "S", Body: "B"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID == "" || n.Status != domain.StatusAccepted {
		t.Fatalf("unexpected: %+v", n)
	}
	if len(n.Results) != 1 || n.Results[0].Status != domain.StatusAccepted {
		t.Fatalf("expected initial accepted result, got %+v", n.Results)
	}
	got, _ := repo.Get(context.Background(), n.ID)
	if got == nil {
		t.Fatal("notification not persisted")
	}
}

func TestServiceUnsupportedChannel(t *testing.T) {
	repo := storage.NewMemory()
	queue := processing.NewQueue(2)
	svc := New(repo, queue, []domain.Channel{domain.ChannelEmail})
	_, err := svc.Create(context.Background(), domain.ChannelSMS,
		[]domain.Recipient{{ID: "u1", Phone: "+14155550123"}},
		domain.Message{Body: "B"},
	)
	if !errors.Is(err, domain.ErrChannelUnsupported) {
		t.Fatalf("expected ErrChannelUnsupported, got %v", err)
	}
}

func TestServiceNoRecipientsValidation(t *testing.T) {
	repo := storage.NewMemory()
	queue := processing.NewQueue(2)
	svc := New(repo, queue, []domain.Channel{domain.ChannelEmail})
	_, err := svc.Create(context.Background(), domain.ChannelEmail, nil, domain.Message{Subject: "S", Body: "B"})
	var ve *domain.ValidationErr
	if !errors.As(err, &ve) || ve.Code != "missing_recipients" {
		t.Fatalf("expected missing_recipients, got %v", err)
	}
}

func TestServiceQueueFullBackpressure(t *testing.T) {
	repo := storage.NewMemory()
	queue := processing.NewQueue(1) // tiny queue; nobody drains it
	svc := New(repo, queue, []domain.Channel{domain.ChannelEmail}, WithClock(func() time.Time { return time.Now().UTC() }))

	// First one fills the queue.
	if _, err := svc.Create(context.Background(), domain.ChannelEmail,
		[]domain.Recipient{{ID: "u1", Email: "a@b.com"}}, domain.Message{Subject: "S", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	// Second one must be rejected with ErrQueueFull.
	_, err := svc.Create(context.Background(), domain.ChannelEmail,
		[]domain.Recipient{{ID: "u2", Email: "b@b.com"}}, domain.Message{Subject: "S", Body: "B"})
	if !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}
