package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
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
)

func setupTestHandler(t *testing.T) (*handler.Handler, *service.NotificationService) {
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
		2,
		10,
	)

	ctx := context.Background()
	notifService.Start(ctx)

	t.Cleanup(func() {
		notifService.Stop()
	})

	h := handler.NewHandler(notifService, log)
	return h, notifService
}

func TestCreateNotification_Success(t *testing.T) {
	h, _ := setupTestHandler(t)

	reqBody := domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "test@example.com"},
		},
		Content: domain.Content{
			Subject: "Test",
			Body:    "Test message",
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, rr.Code)
	}

	var notification domain.Notification
	json.NewDecoder(rr.Body).Decode(&notification)

	if notification.ID == "" {
		t.Error("expected notification ID to be set")
	}

	validStatuses := map[domain.Status]bool{
		domain.StatusPending:  true,
		domain.StatusQueued:   true,
		domain.StatusSending:  true,
		domain.StatusSent:     true,
		domain.StatusFailed:   true,
		domain.StatusPartial:  true,
	}

	if !validStatuses[notification.Status] {
		t.Errorf("unexpected status: %s", notification.Status)
	}
}

func TestCreateNotification_InvalidChannel(t *testing.T) {
	h, _ := setupTestHandler(t)

	reqBody := domain.CreateNotificationRequest{
		Channel: domain.Channel("invalid"),
		Recipients: []domain.Recipient{
			{Value: "test@example.com"},
		},
		Content: domain.Content{
			Body: "Test message",
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestCreateNotification_InvalidRecipient(t *testing.T) {
	h, _ := setupTestHandler(t)

	reqBody := domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "invalid-email"},
		},
		Content: domain.Content{
			Subject: "Test",
			Body:    "Test message",
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestGetNotification_NotFound(t *testing.T) {
	h, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v1/notifications/nonexistent", nil)

	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestHealthCheck(t *testing.T) {
	h, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)

	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]string
	json.NewDecoder(rr.Body).Decode(&response)

	if response["status"] != "healthy" {
		t.Errorf("expected status healthy, got %s", response["status"])
	}
}

func TestListNotifications_Empty(t *testing.T) {
	h, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v1/notifications", nil)

	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&response)

	total := int(response["total"].(float64))
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
}
