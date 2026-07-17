package store

import (
	"context"
	"testing"
	"time"

	"github.com/assessment/notify-service/internal/domain"
)

func TestMemoryStoreSaveAndGet(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()

	n := &domain.Notification{
		ID:        "test-1",
		Channel:   domain.ChannelEmail,
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := st.Save(ctx, n); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := st.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != "test-1" {
		t.Errorf("expected ID test-1, got %s", got.ID)
	}
}

func TestMemoryStoreGetNotFound(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()

	_, err := st.Get(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStoreList(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()

	for i := 0; i < 5; i++ {
		n := &domain.Notification{
			ID:        "test-" + string(rune('0'+i)),
			Channel:   domain.ChannelEmail,
			Status:    domain.StatusPending,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
			UpdatedAt: time.Now(),
		}
		if err := st.Save(ctx, n); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	t.Run("list all", func(t *testing.T) {
		notifs, total, err := st.List(ctx, 0, 10)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if total != 5 {
			t.Errorf("expected total 5, got %d", total)
		}
		if len(notifs) != 5 {
			t.Errorf("expected 5 notifications, got %d", len(notifs))
		}
	})

	t.Run("list with pagination", func(t *testing.T) {
		notifs, total, err := st.List(ctx, 0, 2)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if total != 5 {
			t.Errorf("expected total 5, got %d", total)
		}
		if len(notifs) != 2 {
			t.Errorf("expected 2 notifications, got %d", len(notifs))
		}
	})

	t.Run("list offset beyond total", func(t *testing.T) {
		notifs, total, err := st.List(ctx, 10, 10)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if total != 5 {
			t.Errorf("expected total 5, got %d", total)
		}
		if len(notifs) != 0 {
			t.Errorf("expected 0 notifications, got %d", len(notifs))
		}
	})
}
