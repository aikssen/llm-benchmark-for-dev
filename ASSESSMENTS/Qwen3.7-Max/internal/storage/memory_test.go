package storage

import (
	"context"
	"testing"
	"time"

	"github.com/notifications-service/internal/domain"
)

func TestMemoryStore_SaveAndGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	notification := &domain.Notification{
		ID:        "test-id",
		Channel:   domain.ChannelEmail,
		Subject:   "Test",
		Message:   "Hello",
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := store.Save(ctx, notification)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Get(ctx, "test-id")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ID != notification.ID {
		t.Errorf("Get() ID = %v, want %v", got.ID, notification.ID)
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	if err != domain.ErrNotificationNotFound {
		t.Errorf("Get() error = %v, want %v", err, domain.ErrNotificationNotFound)
	}
}

func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		n := &domain.Notification{
			ID:        "id-" + string(rune('0'+i)),
			Channel:   domain.ChannelEmail,
			Status:    domain.StatusPending,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
			UpdatedAt: time.Now(),
		}
		store.Save(ctx, n)
	}

	results, total, err := store.List(ctx, domain.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if total != 5 {
		t.Errorf("List() total = %v, want 5", total)
	}

	if len(results) != 5 {
		t.Errorf("List() results = %v, want 5", len(results))
	}
}

func TestMemoryStore_ListWithFilter(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	emailChannel := domain.ChannelEmail
	smsChannel := domain.ChannelSMS

	store.Save(ctx, &domain.Notification{
		ID: "1", Channel: emailChannel, Status: domain.StatusPending,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	store.Save(ctx, &domain.Notification{
		ID: "2", Channel: smsChannel, Status: domain.StatusPending,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	results, _, err := store.List(ctx, domain.ListFilter{Channel: &emailChannel, Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("List() with channel filter = %v, want 1", len(results))
	}
}

func TestMemoryStore_UpdateStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	store.Save(ctx, &domain.Notification{
		ID: "test", Status: domain.StatusPending,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	err := store.UpdateStatus(ctx, "test", domain.StatusDelivered)
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	got, _ := store.Get(ctx, "test")
	if got.Status != domain.StatusDelivered {
		t.Errorf("UpdateStatus() status = %v, want %v", got.Status, domain.StatusDelivered)
	}
}

func TestMemoryStore_RecipientStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	rs := &domain.RecipientNotification{
		NotificationID: "notif-1",
		Recipient:      "user@example.com",
		Channel:        domain.ChannelEmail,
		Status:         domain.StatusDelivered,
	}

	err := store.SaveRecipientStatus(ctx, rs)
	if err != nil {
		t.Fatalf("SaveRecipientStatus() error = %v", err)
	}

	statuses, err := store.GetRecipientStatuses(ctx, "notif-1")
	if err != nil {
		t.Fatalf("GetRecipientStatuses() error = %v", err)
	}

	if len(statuses) != 1 {
		t.Errorf("GetRecipientStatuses() = %v, want 1", len(statuses))
	}
}
