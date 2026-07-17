package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"notificationservice/internal/domain"
)

func newNotification(id string, ch domain.Channel, deliveries ...domain.Delivery) domain.Notification {
	return domain.Notification{
		ID:         id,
		Channel:    ch,
		Message:    domain.Message{Subject: "s", Body: "b"},
		CreatedAt:  time.Now(),
		Deliveries: deliveries,
	}
}

func TestMemoryCreateAndGet(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	n := newNotification("ntf_1", domain.ChannelEmail,
		domain.Delivery{ID: "dlv_1", Recipient: "a@example.com", Status: domain.DeliveryPending})

	if err := m.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if err := m.CreateNotification(ctx, n); err == nil {
		t.Fatal("creating a duplicate ID should fail")
	}

	got, err := m.GetNotification(ctx, "ntf_1")
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got.ID != "ntf_1" || len(got.Deliveries) != 1 {
		t.Fatalf("unexpected notification: %+v", got)
	}

	// Mutating the returned copy must not affect the stored value.
	got.Deliveries[0].Status = domain.DeliveryFailed
	again, _ := m.GetNotification(ctx, "ntf_1")
	if again.Deliveries[0].Status != domain.DeliveryPending {
		t.Error("store leaked mutable state to the caller")
	}

	if _, err := m.GetNotification(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryDelete(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	if err := m.CreateNotification(ctx, newNotification("ntf_1", domain.ChannelSMS)); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteNotification(ctx, "ntf_1"); err != nil {
		t.Fatalf("DeleteNotification: %v", err)
	}
	if _, err := m.GetNotification(ctx, "ntf_1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted notification should not be found, got %v", err)
	}
	if err := m.DeleteNotification(ctx, "ntf_1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting missing notification should return ErrNotFound, got %v", err)
	}

	res, err := m.ListNotifications(ctx, ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Errorf("list after delete: total = %d, want 0", res.Total)
	}
}

func TestMemoryUpdateDelivery(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m.now = func() time.Time { return fixed }

	n := newNotification("ntf_1", domain.ChannelPush,
		domain.Delivery{ID: "dlv_1", Status: domain.DeliveryPending})
	if err := m.CreateNotification(ctx, n); err != nil {
		t.Fatal(err)
	}

	updated, err := m.UpdateDelivery(ctx, "ntf_1", "dlv_1", func(d *domain.Delivery) {
		d.Status = domain.DeliveryDelivered
		d.Attempts = 2
	})
	if err != nil {
		t.Fatalf("UpdateDelivery: %v", err)
	}
	if updated.Status != domain.DeliveryDelivered || updated.Attempts != 2 {
		t.Errorf("update not applied: %+v", updated)
	}
	if !updated.UpdatedAt.Equal(fixed) {
		t.Errorf("UpdatedAt = %v, want %v", updated.UpdatedAt, fixed)
	}

	if _, err := m.UpdateDelivery(ctx, "ntf_1", "missing", func(*domain.Delivery) {}); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing delivery, got %v", err)
	}
	if _, err := m.UpdateDelivery(ctx, "missing", "dlv_1", func(*domain.Delivery) {}); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing notification, got %v", err)
	}
}

func TestMemoryList(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	for i := range 5 {
		ch := domain.ChannelEmail
		if i%2 == 1 {
			ch = domain.ChannelSMS
		}
		status := domain.DeliveryPending
		if i == 4 {
			status = domain.DeliveryDelivered
		}
		n := newNotification(fmt.Sprintf("ntf_%d", i), ch, domain.Delivery{ID: "d", Status: status})
		if err := m.CreateNotification(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("newest first with pagination", func(t *testing.T) {
		res, err := m.ListNotifications(ctx, ListFilter{Limit: 2, Offset: 1})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 5 {
			t.Errorf("total = %d, want 5", res.Total)
		}
		if len(res.Notifications) != 2 || res.Notifications[0].ID != "ntf_3" || res.Notifications[1].ID != "ntf_2" {
			t.Errorf("unexpected page: %+v", res.Notifications)
		}
	})

	t.Run("filter by channel", func(t *testing.T) {
		res, err := m.ListNotifications(ctx, ListFilter{Limit: 10, Channel: domain.ChannelSMS})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 2 {
			t.Errorf("total = %d, want 2", res.Total)
		}
	})

	t.Run("filter by aggregate status", func(t *testing.T) {
		res, err := m.ListNotifications(ctx, ListFilter{Limit: 10, Status: domain.StatusDelivered})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 1 || res.Notifications[0].ID != "ntf_4" {
			t.Errorf("unexpected result: %+v", res)
		}
	})

	t.Run("offset beyond matches", func(t *testing.T) {
		res, err := m.ListNotifications(ctx, ListFilter{Limit: 10, Offset: 99})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 5 || len(res.Notifications) != 0 {
			t.Errorf("unexpected result: %+v", res)
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		if _, err := m.ListNotifications(ctx, ListFilter{Limit: 0}); err == nil {
			t.Error("limit 0 should be rejected")
		}
	})
}
