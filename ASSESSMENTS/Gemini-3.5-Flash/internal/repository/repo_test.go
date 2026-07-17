package repository

import (
	"context"
	"testing"
	"time"

	"notification-service/internal/domain"
)

func TestInMemoryRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()

	// 1. Test Save & Get
	n1 := &domain.Notification{
		ID:        "notify-1",
		RequestID: "req-123",
		Recipient: "user1@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Hello 1",
		Status:    domain.StatusPending,
	}

	err := repo.Save(ctx, n1)
	if err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	if n1.CreatedAt.IsZero() || n1.UpdatedAt.IsZero() {
		t.Errorf("expected timestamps to be set, got CreatedAt: %v, UpdatedAt: %v", n1.CreatedAt, n1.UpdatedAt)
	}

	fetched, err := repo.Get(ctx, "notify-1")
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}

	if fetched.ID != n1.ID || fetched.Content != n1.Content || fetched.Status != n1.Status {
		t.Errorf("fetched notification fields mismatch: %+v", fetched)
	}

	// 2. Test Get NotFound
	_, err = repo.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent ID, got nil")
	}

	// 3. Test Deep Copy Safety
	fetched.Content = "Mutated Content"
	afterMutation, err := repo.Get(ctx, "notify-1")
	if err != nil {
		t.Fatalf("failed to get second time: %v", err)
	}
	if afterMutation.Content == "Mutated Content" {
		t.Error("repository internal state was mutated; deep copy failed")
	}

	// 4. Test Update
	n1.Status = domain.StatusSent
	err = repo.Update(ctx, n1)
	if err != nil {
		t.Fatalf("failed to update: %v", err)
	}

	updated, err := repo.Get(ctx, "notify-1")
	if err != nil {
		t.Fatalf("failed to get updated: %v", err)
	}
	if updated.Status != domain.StatusSent {
		t.Errorf("expected status to be StatusSent, got %v", updated.Status)
	}

	// 5. Test List and Sorting
	n2 := &domain.Notification{
		ID:        "notify-2",
		RequestID: "req-123",
		Recipient: "+15550000000",
		Channel:   domain.ChannelSMS,
		Content:   "Hello 2",
		Status:    domain.StatusPending,
	}
	// Introduce a tiny sleep so timestamps are distinct
	time.Sleep(10 * time.Millisecond)
	err = repo.Save(ctx, n2)
	if err != nil {
		t.Fatalf("failed to save second notification: %v", err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("expected list length of 2, got %d", len(list))
	}

	// List should be sorted by CreatedAt descending (newest first, so notify-2 must be first)
	if list[0].ID != "notify-2" {
		t.Errorf("expected newest notification (notify-2) to be first, got %s", list[0].ID)
	}
}
