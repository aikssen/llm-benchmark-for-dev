package store

import (
	"context"
	"testing"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
)

func TestMemoryStoreCRUD(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	n := &domain.Notification{
		ID:        "n1",
		RequestID: "r1",
		Recipient: "a@b.com",
		Channel:   domain.ChannelEmail,
		Body:      "hi",
		Status:    domain.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Create(ctx, n); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Recipient != "a@b.com" {
		t.Fatalf("got %+v", got)
	}

	got.Status = domain.StatusDelivered
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}

	got2, err := s.Get(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if got2.Status != domain.StatusDelivered {
		t.Fatalf("status=%s", got2.Status)
	}

	items, total, err := s.List(ctx, domain.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}

	_, err = s.Get(ctx, "missing")
	if err != ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestMemoryStoreListFilters(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = s.Create(ctx, &domain.Notification{ID: "1", Channel: domain.ChannelEmail, Status: domain.StatusPending, CreatedAt: now, UpdatedAt: now})
	_ = s.Create(ctx, &domain.Notification{ID: "2", Channel: domain.ChannelSMS, Status: domain.StatusDelivered, CreatedAt: now, UpdatedAt: now})
	_ = s.Create(ctx, &domain.Notification{ID: "3", Channel: domain.ChannelEmail, Status: domain.StatusFailed, CreatedAt: now, UpdatedAt: now})

	items, total, err := s.List(ctx, domain.ListFilter{Channel: domain.ChannelEmail, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}

	items, total, err = s.List(ctx, domain.ListFilter{Status: domain.StatusDelivered, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || items[0].ID != "2" {
		t.Fatalf("unexpected list result total=%d items=%v", total, items)
	}
}
