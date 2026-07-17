package storage

import (
	"context"
	"testing"

	"github.com/example/notification-service/internal/domain"
)

func newTestNotification(id domain.ID) *domain.Notification {
	return &domain.Notification{
		ID:         id,
		Channel:    domain.ChannelEmail,
		Status:     domain.StatusAccepted,
		Recipients: []domain.Recipient{{ID: "u1"}, {ID: "u2"}},
	}
}

func TestMemoryCreateGet(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	n := newTestNotification("n1")
	if err := m.Create(ctx, n); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "n1" {
		t.Fatalf("got %s", got.ID)
	}
	if _, err := m.Get(ctx, "missing"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestMemoryListPaginatesNewestFirst(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = m.Create(ctx, newTestNotification(domain.ID(rune('a'+i))))
	}
	items, total, err := m.List(ctx, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(items) != 2 {
		t.Fatalf("expected total=5, len=2; got %d %d", total, len(items))
	}
	// Newest first: last created is 'e'.
	if items[0].ID != "e" {
		t.Fatalf("expected newest first (e), got %s", items[0].ID)
	}
	items2, _, _ := m.List(ctx, 2, 2)
	if items2[0].ID != "c" {
		t.Fatalf("expected c at offset 2, got %s", items2[0].ID)
	}
}

func TestMemorySetStatusAndUpsertResult(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.Create(ctx, newTestNotification("n1"))

	if err := m.SetStatus(ctx, "n1", domain.StatusProcessing); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(ctx, "n1")
	if got.Status != domain.StatusProcessing {
		t.Fatalf("expected processing, got %s", got.Status)
	}

	// Insert a new result.
	if err := m.UpsertResult(ctx, "n1", domain.RecipientResult{RecipientID: "u1", Status: domain.StatusDelivered}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Get(ctx, "n1")
	if len(got.Results) != 1 || got.Results[0].Status != domain.StatusDelivered {
		t.Fatalf("unexpected results: %+v", got.Results)
	}

	// Update existing result.
	if err := m.UpsertResult(ctx, "n1", domain.RecipientResult{RecipientID: "u1", Status: domain.StatusFailed, LastError: "boom"}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Get(ctx, "n1")
	if len(got.Results) != 1 || got.Results[0].Status != domain.StatusFailed || got.Results[0].LastError != "boom" {
		t.Fatalf("unexpected results: %+v", got.Results)
	}
}

func TestMemoryReturnsCopy(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.Create(ctx, newTestNotification("n1"))
	got, _ := m.Get(ctx, "n1")
	got.Status = domain.StatusFailed
	again, _ := m.Get(ctx, "n1")
	if again.Status == domain.StatusFailed {
		t.Fatal("Get must return a copy, not the internal pointer")
	}
}
