package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/notifications-service/internal/domain"
	"github.com/notifications-service/internal/provider"
	"github.com/notifications-service/internal/service"
	"github.com/notifications-service/internal/storage"
	"github.com/notifications-service/internal/validator"
)

func setupTestServer(t *testing.T) (*http.ServeMux, *service.NotificationService) {
	t.Helper()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	store := storage.NewMemoryStore()
	registry := provider.NewRegistry()
	registry.Register(provider.NewSimulatedProvider(domain.ChannelEmail, logger))
	registry.Register(provider.NewSimulatedProvider(domain.ChannelSMS, logger))
	registry.Register(provider.NewSimulatedProvider(domain.ChannelPush, logger))

	chain := validator.NewChain(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
		validator.NewRateLimitValidator(100, time.Minute),
	)

	processor := service.NewProcessor(store, registry, chain, logger, 2, 100)
	processor.Start()
	t.Cleanup(processor.Stop)

	svc := service.NewNotificationService(store, registry, chain, processor, logger)
	router := NewRouter(svc, logger)

	mux := http.NewServeMux()
	mux.Handle("/", router)

	return mux, svc
}

func TestHealthCheck(t *testing.T) {
	router, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HealthCheck status = %v, want %v", w.Code, http.StatusOK)
	}

	var response map[string]string
	json.NewDecoder(w.Body).Decode(&response)

	if response["status"] != "healthy" {
		t.Errorf("HealthCheck status = %v, want healthy", response["status"])
	}
}

func TestSubmitNotification(t *testing.T) {
	router, _ := setupTestServer(t)

	body := map[string]interface{}{
		"channel":    "email",
		"subject":    "Test Subject",
		"message":    "Test message",
		"recipients": []string{"user@example.com"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("SubmitNotification status = %v, want %v", w.Code, http.StatusAccepted)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	if response["id"] == nil {
		t.Error("SubmitNotification should return notification ID")
	}

	if response["status"] != "pending" {
		t.Errorf("SubmitNotification status = %v, want pending", response["status"])
	}
}

func TestSubmitNotification_InvalidChannel(t *testing.T) {
	router, _ := setupTestServer(t)

	body := map[string]interface{}{
		"channel":    "invalid",
		"message":    "Test",
		"recipients": []string{"user@example.com"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("SubmitNotification invalid channel status = %v, want %v", w.Code, http.StatusBadRequest)
	}
}

func TestSubmitNotification_EmptyRecipients(t *testing.T) {
	router, _ := setupTestServer(t)

	body := map[string]interface{}{
		"channel":    "email",
		"subject":    "Test",
		"message":    "Test",
		"recipients": []string{},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("SubmitNotification empty recipients status = %v, want %v", w.Code, http.StatusBadRequest)
	}
}

func TestGetNotification(t *testing.T) {
	router, svc := setupTestServer(t)

	submitBody := map[string]interface{}{
		"channel":    "sms",
		"message":    "Test SMS",
		"recipients": []string{"+1234567890"},
	}
	jsonBody, _ := json.Marshal(submitBody)

	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var submitResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&submitResp)
	id := submitResp["id"].(string)

	_ = svc

	getReq := httptest.NewRequest("GET", "/api/v1/notifications/"+id, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("GetNotification status = %v, want %v", getW.Code, http.StatusOK)
	}
}

func TestGetNotification_NotFound(t *testing.T) {
	router, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/notifications/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetNotification not found status = %v, want %v", w.Code, http.StatusNotFound)
	}
}

func TestListNotifications(t *testing.T) {
	router, _ := setupTestServer(t)

	for i := 0; i < 3; i++ {
		body := map[string]interface{}{
			"channel":    "email",
			"subject":    "Test",
			"message":    "Message",
			"recipients": []string{"user@example.com"},
		}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	listReq := httptest.NewRequest("GET", "/api/v1/notifications?limit=10", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("ListNotifications status = %v, want %v", listW.Code, http.StatusOK)
	}

	var response map[string]interface{}
	json.NewDecoder(listW.Body).Decode(&response)

	total := int(response["total"].(float64))
	if total != 3 {
		t.Errorf("ListNotifications total = %v, want 3", total)
	}
}

func TestListNotifications_WithChannelFilter(t *testing.T) {
	router, _ := setupTestServer(t)

	emailBody := map[string]interface{}{
		"channel":    "email",
		"subject":    "Test",
		"message":    "Email message",
		"recipients": []string{"user@example.com"},
	}
	jsonBody, _ := json.Marshal(emailBody)
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	smsBody := map[string]interface{}{
		"channel":    "sms",
		"message":    "SMS message",
		"recipients": []string{"+1234567890"},
	}
	jsonBody, _ = json.Marshal(smsBody)
	req = httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	listReq := httptest.NewRequest("GET", "/api/v1/notifications?channel=email", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)

	var response map[string]interface{}
	json.NewDecoder(listW.Body).Decode(&response)

	total := int(response["total"].(float64))
	if total != 1 {
		t.Errorf("ListNotifications with email filter total = %v, want 1", total)
	}
}
