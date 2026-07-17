package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

func mkNotif(id string, ch domain.Channel, created time.Time) *domain.Notification {
	return &domain.Notification{ID: id, Channel: ch, Status: domain.StatusPending, CreatedAt: created}
}

func TestMemory_CreateGetUpdate(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	n := mkNotif("1", domain.ChannelEmail, time.Now())

	if err := m.Create(ctx, n); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Duplicate create is rejected.
	if err := m.Create(ctx, n); err == nil {
		t.Error("duplicate create should fail")
	}

	got, err := m.Get(ctx, "1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "1" {
		t.Errorf("wrong id: %s", got.ID)
	}

	// Mutating the returned copy must not affect stored state.
	got.Status = domain.StatusDelivered
	again, _ := m.Get(ctx, "1")
	if again.Status != domain.StatusPending {
		t.Error("store returned a shared pointer; state leaked")
	}

	n.Status = domain.StatusDelivered
	if err := m.Update(ctx, n); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := m.Get(ctx, "1")
	if updated.Status != domain.StatusDelivered {
		t.Error("update did not persist")
	}
}

func TestMemory_GetNotFound(t *testing.T) {
	_, err := NewMemory().Get(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemory_UpdateNotFound(t *testing.T) {
	err := NewMemory().Update(context.Background(), mkNotif("x", domain.ChannelEmail, time.Now()))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemory_ListFilterAndSort(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	base := time.Unix(1000, 0)

	// Three email, two sms, increasing timestamps.
	for i := range 3 {
		_ = m.Create(ctx, mkNotif(fmt.Sprintf("e%d", i), domain.ChannelEmail, base.Add(time.Duration(i)*time.Second)))
	}
	for i := range 2 {
		_ = m.Create(ctx, mkNotif(fmt.Sprintf("s%d", i), domain.ChannelSMS, base.Add(time.Duration(i)*time.Second)))
	}

	items, total, err := m.List(ctx, ListFilter{Channel: domain.ChannelEmail})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("expected 3 email notifications, got total=%d len=%d", total, len(items))
	}
	// Newest first.
	if items[0].ID != "e2" || items[2].ID != "e0" {
		t.Errorf("wrong sort order: %s ... %s", items[0].ID, items[2].ID)
	}
}

func TestMemory_ListPagination(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	base := time.Unix(1000, 0)
	for i := range 10 {
		_ = m.Create(ctx, mkNotif(fmt.Sprintf("%02d", i), domain.ChannelEmail, base.Add(time.Duration(i)*time.Second)))
	}

	items, total, _ := m.List(ctx, ListFilter{Limit: 3, Offset: 2})
	if total != 10 {
		t.Errorf("total should reflect all matches, got %d", total)
	}
	if len(items) != 3 {
		t.Errorf("expected page size 3, got %d", len(items))
	}

	// Offset beyond the end yields an empty page, not an error.
	empty, _, _ := m.List(ctx, ListFilter{Offset: 100})
	if len(empty) != 0 {
		t.Errorf("expected empty page, got %d", len(empty))
	}
}

// TestMemory_ConcurrentAccess runs under `go test -race` to catch data races.
func TestMemory_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("%d", i)
			_ = m.Create(ctx, mkNotif(id, domain.ChannelEmail, time.Unix(int64(i), 0)))
			_, _ = m.Get(ctx, id)
			_, _, _ = m.List(ctx, ListFilter{})
		}(i)
	}
	wg.Wait()
}
