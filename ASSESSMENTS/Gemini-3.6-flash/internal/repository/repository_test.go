package repository

import (
	"context"
	"testing"
	"time"

	"notification-service/internal/domain"
)

func TestMemoryRepository_CRUDAndPagination(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	req := &domain.NotificationRequest{
		ID:              "req-1",
		Channel:         domain.ChannelEmail,
		Recipients:      []string{"a@example.com", "b@example.com"},
		Body:            "Hello",
		Status:          domain.StatusPending,
		TotalRecipients: 2,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.SaveRequest(ctx, req); err != nil {
		t.Fatalf("failed to save request: %v", err)
	}

	rec1 := &domain.NotificationRecipientStatus{
		ID:        "rec-1",
		RequestID: "req-1",
		Recipient: "a@example.com",
		Channel:   domain.ChannelEmail,
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
	}
	rec2 := &domain.NotificationRecipientStatus{
		ID:        "rec-2",
		RequestID: "req-1",
		Recipient: "b@example.com",
		Channel:   domain.ChannelEmail,
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
	}

	_ = repo.SaveRecipientStatus(ctx, rec1)
	_ = repo.SaveRecipientStatus(ctx, rec2)

	fetched, err := repo.GetRequestByID(ctx, "req-1")
	if err != nil {
		t.Fatalf("failed to get request by id: %v", err)
	}
	if len(fetched.RecipientDetail) != 2 {
		t.Fatalf("expected 2 recipient details, got %d", len(fetched.RecipientDetail))
	}

	// Update recipient statuses
	_ = repo.UpdateRecipientStatus(ctx, "rec-1", domain.StatusDelivered, "email-provider-1", "", 1)
	_ = repo.UpdateRecipientStatus(ctx, "rec-2", domain.StatusFailed, "", "opted out", 1)
	_ = repo.UpdateAggregateStatus(ctx, "req-1")

	updatedReq, _ := repo.GetRequestByID(ctx, "req-1")
	if updatedReq.Status != domain.StatusPartiallyFailed {
		t.Fatalf("expected status partially_failed, got %s", updatedReq.Status)
	}
	if updatedReq.DeliveredCount != 1 || updatedReq.FailedCount != 1 {
		t.Fatalf("expected 1 delivered and 1 failed, got %d delivered, %d failed", updatedReq.DeliveredCount, updatedReq.FailedCount)
	}

	// Test pagination & filtering
	list, err := repo.ListRequests(ctx, domain.NotificationFilter{
		Channel: domain.ChannelEmail,
		Limit:   10,
		Offset:  0,
	})
	if err != nil {
		t.Fatalf("failed to list requests: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 item in list, got %d", list.Total)
	}
}
