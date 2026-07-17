package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/notification-service/internal/domain"
	"github.com/notification-service/internal/repository"
)

func TestMemoryRepository_Save(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	notification := &domain.Notification{
		ID:         "test-1",
		Recipients: []domain.Recipient{{Value: "test@example.com"}},
		Channel:    domain.ChannelEmail,
		Content:    domain.Content{Body: "test"},
		Status:     domain.StatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := repo.Save(ctx, notification)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	saved, err := repo.GetByID(ctx, "test-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if saved.ID != notification.ID {
		t.Errorf("expected ID %s, got %s", notification.ID, saved.ID)
	}
}

func TestMemoryRepository_GetByID_NotFound(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMemoryRepository_Update(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	notification := &domain.Notification{
		ID:         "test-1",
		Recipients: []domain.Recipient{{Value: "test@example.com"}},
		Channel:    domain.ChannelEmail,
		Content:    domain.Content{Body: "test"},
		Status:     domain.StatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	repo.Save(ctx, notification)

	notification.Status = domain.StatusSent
	err := repo.Update(ctx, notification)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updated, _ := repo.GetByID(ctx, "test-1")
	if updated.Status != domain.StatusSent {
		t.Errorf("expected status %s, got %s", domain.StatusSent, updated.Status)
	}
}

func TestMemoryRepository_List(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		repo.Save(ctx, &domain.Notification{
			ID:        fmt.Sprintf("test-%d", i),
			Channel:   domain.ChannelEmail,
			Status:    domain.StatusSent,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	params := domain.ListNotificationsParams{
		Limit:  3,
		Offset: 0,
	}

	notifications, total, err := repo.List(ctx, params)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}

	if len(notifications) != 3 {
		t.Errorf("expected 3 notifications, got %d", len(notifications))
	}
}

func TestMemoryRepository_List_FilterByChannel(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	repo.Save(ctx, &domain.Notification{
		ID:      "email-1",
		Channel: domain.ChannelEmail,
		Status:  domain.StatusSent,
	})

	repo.Save(ctx, &domain.Notification{
		ID:      "sms-1",
		Channel: domain.ChannelSMS,
		Status:  domain.StatusSent,
	})

	params := domain.ListNotificationsParams{
		Channel: domain.ChannelEmail,
		Limit:   10,
	}

	notifications, total, err := repo.List(ctx, params)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}

	if len(notifications) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notifications))
	}

	if notifications[0].Channel != domain.ChannelEmail {
		t.Errorf("expected channel email, got %s", notifications[0].Channel)
	}
}
