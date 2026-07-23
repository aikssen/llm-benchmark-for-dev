package service

import (
	"context"
	"testing"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/validator"
	"notification-service/pkg/resilience"
)

func TestService_SubmitNotification_FullFlow(t *testing.T) {
	repo := repository.NewMemoryRepository()

	// Pipeline with recipient & content validators
	pipeline := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
	)

	// Provider Registry with mock Email provider
	reg := provider.NewRegistry()
	mockEmail := provider.NewSimulatedEmailProvider("email-mock", 0.0, 5*time.Millisecond)
	reg.Register(mockEmail, resilience.RetryConfig{MaxAttempts: 1}, nil)

	svc := NewService(repo, pipeline, reg, 4, 100)
	defer svc.Stop()

	input := SubmitNotificationInput{
		Channel:    domain.ChannelEmail,
		Recipients: []string{"valid1@example.com", "invalid-email"},
		Body:       "Test notification body",
	}

	req, err := svc.SubmitNotification(context.Background(), input)
	if err != nil {
		t.Fatalf("failed to submit notification: %v", err)
	}

	if req.ID == "" {
		t.Fatalf("expected valid UUID request ID")
	}

	// Wait briefly for background processing
	time.Sleep(100 * time.Millisecond)

	status, err := svc.GetNotificationStatus(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("failed to get notification status: %v", err)
	}

	if status.Status != domain.StatusPartiallyFailed {
		t.Fatalf("expected status partially_failed, got %s", status.Status)
	}

	if status.DeliveredCount != 1 || status.FailedCount != 1 {
		t.Fatalf("expected 1 delivered and 1 failed, got %d delivered, %d failed", status.DeliveredCount, status.FailedCount)
	}
}
