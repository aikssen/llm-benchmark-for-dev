package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/notification-service/internal/domain"
	"github.com/notification-service/internal/handler"
	"github.com/notification-service/internal/provider"
	"github.com/notification-service/internal/repository"
	"github.com/notification-service/internal/service"
	"github.com/notification-service/internal/validator"
	"github.com/notification-service/pkg/logger"
	"context"
)

func setupIntegrationServer(t *testing.T) (*httptest.Server, *service.NotificationService) {
	t.Helper()

	log := logger.New("error")
	repo := repository.NewMemoryRepository()

	providerRegistry := provider.NewRegistry()
	providerRegistry.Register(provider.NewEmailProvider(log))
	providerRegistry.Register(provider.NewSMSProvider(log))
	providerRegistry.Register(provider.NewPushProvider(log))

	pipeline := validator.NewPipeline(log,
		validator.NewChannelValidator(),
		validator.NewRecipientValidator(),
		validator.NewContentValidator(10000),
		validator.NewRateLimiterValidator(100, time.Minute),
	)

	notifService := service.NewNotificationService(
		repo,
		providerRegistry,
		pipeline,
		log,
		3,
		50,
	)

	ctx := context.Background()
	notifService.Start(ctx)

	router := mux.NewRouter()
	h := handler.NewHandler(notifService, log)
	h.RegisterRoutes(router)

	server := httptest.NewServer(router)

	t.Cleanup(func() {
		server.Close()
		notifService.Stop()
	})

	return server, notifService
}

func TestIntegration_CreateAndGetNotification(t *testing.T) {
	server, _ := setupIntegrationServer(t)

	reqBody := domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "user@example.com"},
		},
		Content: domain.Content{
			Subject: "Integration Test",
			Body:    "This is a test notification",
		},
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(server.URL+"/api/v1/notifications", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
	}

	var notification domain.Notification
	json.NewDecoder(resp.Body).Decode(&notification)

	if notification.ID == "" {
		t.Fatal("expected notification ID to be set")
	}

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Get the notification
	getResp, err := http.Get(fmt.Sprintf("%s/api/v1/notifications/%s", server.URL, notification.ID))
	if err != nil {
		t.Fatalf("failed to get notification: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, getResp.StatusCode)
	}

	var fetched domain.Notification
	json.NewDecoder(getResp.Body).Decode(&fetched)

	if fetched.ID != notification.ID {
		t.Errorf("expected ID %s, got %s", notification.ID, fetched.ID)
	}

	if fetched.Status != domain.StatusSent && fetched.Status != domain.StatusSending {
		t.Logf("notification status: %s (may still be processing)", fetched.Status)
	}
}

func TestIntegration_CreateMultipleRecipients(t *testing.T) {
	server, _ := setupIntegrationServer(t)

	reqBody := domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "user1@example.com"},
			{Value: "user2@example.com"},
			{Value: "user3@example.com"},
		},
		Content: domain.Content{
			Subject: "Bulk Test",
			Body:    "This is a bulk notification test",
		},
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(server.URL+"/api/v1/notifications", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
	}

	var notification domain.Notification
	json.NewDecoder(resp.Body).Decode(&notification)

	if len(notification.Recipients) != 3 {
		t.Errorf("expected 3 recipients, got %d", len(notification.Recipients))
	}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Get the notification
	getResp, err := http.Get(fmt.Sprintf("%s/api/v1/notifications/%s", server.URL, notification.ID))
	if err != nil {
		t.Fatalf("failed to get notification: %v", err)
	}
	defer getResp.Body.Close()

	var fetched domain.Notification
	json.NewDecoder(getResp.Body).Decode(&fetched)

	if len(fetched.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(fetched.Results))
	}
}

func TestIntegration_ListNotifications(t *testing.T) {
	server, _ := setupIntegrationServer(t)

	// Create multiple notifications
	for i := 0; i < 5; i++ {
		reqBody := domain.CreateNotificationRequest{
			Channel: domain.ChannelEmail,
			Recipients: []domain.Recipient{
				{Value: fmt.Sprintf("user%d@example.com", i)},
			},
			Content: domain.Content{
				Subject: fmt.Sprintf("Test %d", i),
				Body:    fmt.Sprintf("Test notification %d", i),
			},
		}

		body, _ := json.Marshal(reqBody)
		resp, _ := http.Post(server.URL+"/api/v1/notifications", "application/json", bytes.NewReader(body))
		resp.Body.Close()
	}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// List notifications
	resp, err := http.Get(server.URL + "/api/v1/notifications?limit=3")
	if err != nil {
		t.Fatalf("failed to list notifications: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	total := int(result["total"].(float64))
	if total < 5 {
		t.Errorf("expected total >= 5, got %d", total)
	}

	notifications := result["notifications"].([]interface{})
	if len(notifications) > 3 {
		t.Errorf("expected <= 3 notifications, got %d", len(notifications))
	}
}

func TestIntegration_InvalidRequest(t *testing.T) {
	server, _ := setupIntegrationServer(t)

	// Missing required fields
	reqBody := map[string]interface{}{
		"channel": "email",
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(server.URL+"/api/v1/notifications", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestIntegration_HealthCheck(t *testing.T) {
	server, _ := setupIntegrationServer(t)

	resp, err := http.Get(server.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if result["status"] != "healthy" {
		t.Errorf("expected status healthy, got %s", result["status"])
	}
}
