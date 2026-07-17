package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/assessment/notify-service/internal/domain"
	"github.com/assessment/notify-service/internal/provider"
	"github.com/assessment/notify-service/internal/store"
	"github.com/assessment/notify-service/internal/validator"
)

func TestOrchestratorSubmit(t *testing.T) {
	st := store.NewMemoryStore()
	reg := provider.NewRegistry()
	reg.Register(provider.NewEmailProvider())

	pipe := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
	)

	cfg := Config{
		MaxWorkers:    2,
		MaxRetries:    1,
		RetryBackoff:  10 * time.Millisecond,
		RequestBuffer: 10,
	}

	orch := New(cfg, st, reg, pipe)
	defer orch.Shutdown()

	n := &domain.Notification{
		ID:        "test-orch-1",
		Channel:   domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "user@test.com"}},
		Message:   domain.Message{Body: "Hello World"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := orch.Submit(context.Background(), n)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	got, err := st.Get(context.Background(), "test-orch-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Status == domain.StatusPending {
		t.Error("expected status to progress beyond pending")
	}
}

func TestOrchestratorValidationFailure(t *testing.T) {
	st := store.NewMemoryStore()
	reg := provider.NewRegistry()

	pipe := validator.NewPipeline(
		validator.NewRecipientValidator(),
	)

	cfg := Config{
		MaxWorkers:    1,
		MaxRetries:    0,
		RetryBackoff:  10 * time.Millisecond,
		RequestBuffer: 5,
	}

	orch := New(cfg, st, reg, pipe)
	defer orch.Shutdown()

	n := &domain.Notification{
		ID:        "test-orch-2",
		Channel:   domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "invalid-email"}},
		Message:   domain.Message{Body: "Hello"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := orch.Submit(context.Background(), n)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	got, err := st.Get(context.Background(), "test-orch-2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Status != domain.StatusValidationErr {
		t.Errorf("expected status validation_error, got %s", got.Status)
	}
}

func TestOrchestratorMultipleRecipients(t *testing.T) {
	st := store.NewMemoryStore()
	reg := provider.NewRegistry()
	reg.Register(provider.NewEmailProvider())

	pipe := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
	)

	cfg := Config{
		MaxWorkers:    2,
		MaxRetries:    1,
		RetryBackoff:  10 * time.Millisecond,
		RequestBuffer: 10,
	}

	orch := New(cfg, st, reg, pipe)
	defer orch.Shutdown()

	n := &domain.Notification{
		ID:      "test-orch-3",
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{ID: "user1@test.com"},
			{ID: "user2@test.com"},
		},
		Message:   domain.Message{Body: "Hello Team"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := orch.Submit(context.Background(), n)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	time.Sleep(time.Second)

	got, err := st.Get(context.Background(), "test-orch-3")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(got.DeliveryResults) != 2 {
		t.Errorf("expected 2 delivery results, got %d", len(got.DeliveryResults))
	}
}
