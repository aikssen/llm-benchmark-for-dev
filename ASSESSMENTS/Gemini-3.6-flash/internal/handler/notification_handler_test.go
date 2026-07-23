package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/service"
	"notification-service/internal/validator"
	"notification-service/pkg/resilience"
)

func setupTestServer() (*httptest.Server, *service.Service) {
	repo := repository.NewMemoryRepository()
	pipeline := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
	)
	reg := provider.NewRegistry()
	mockEmail := provider.NewSimulatedEmailProvider("email-primary", 0.0, 0)
	reg.Register(mockEmail, resilience.RetryConfig{MaxAttempts: 1}, nil)

	svc := service.NewService(repo, pipeline, reg, 4, 100)
	h := NewNotificationHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/notifications", h.Submit)
	mux.HandleFunc("GET /api/v1/notifications/{id}", h.GetStatus)
	mux.HandleFunc("GET /api/v1/notifications", h.List)

	ts := httptest.NewServer(mux)
	return ts, svc
}

func TestHTTP_SubmitAndGetStatus(t *testing.T) {
	ts, svc := setupTestServer()
	defer ts.Close()
	defer svc.Stop()

	client := ts.Client()

	// 1. Submit Request
	payload := map[string]interface{}{
		"channel":    "email",
		"recipients": []string{"alice@example.com", "bob@example.com"},
		"subject":    "Test Alert",
		"body":       "This is a test notification body.",
	}
	bodyBytes, _ := json.Marshal(payload)

	res, err := client.Post(ts.URL+"/api/v1/notifications", "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		t.Fatalf("failed to post request: %v", err)
	}

	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("expected HTTP 202 Accepted, got %d", res.StatusCode)
	}

	var submitResp domain.NotificationRequest
	if err := json.NewDecoder(res.Body).Decode(&submitResp); err != nil {
		t.Fatalf("failed to decode submit response: %v", err)
	}

	if submitResp.ID == "" {
		t.Fatalf("expected non-empty request ID")
	}

	// 2. Wait for processing
	time.Sleep(100 * time.Millisecond)

	// 3. Fetch status
	statusRes, err := client.Get(ts.URL + "/api/v1/notifications/" + submitResp.ID)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if statusRes.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d", statusRes.StatusCode)
	}

	var fetchedResp domain.NotificationRequest
	_ = json.NewDecoder(statusRes.Body).Decode(&fetchedResp)

	if fetchedResp.Status != domain.StatusDelivered {
		t.Fatalf("expected status delivered, got %s", fetchedResp.Status)
	}
	if fetchedResp.DeliveredCount != 2 {
		t.Fatalf("expected 2 delivered recipients, got %d", fetchedResp.DeliveredCount)
	}
}

func TestHTTP_ListNotifications(t *testing.T) {
	ts, svc := setupTestServer()
	defer ts.Close()
	defer svc.Stop()

	// Submit sample request
	_, _ = svc.SubmitNotification(context.Background(), service.SubmitNotificationInput{
		Channel:    domain.ChannelEmail,
		Recipients: []string{"test@example.com"},
		Body:       "Sample body",
	})

	client := ts.Client()
	res, err := client.Get(ts.URL + "/api/v1/notifications?channel=email&limit=10")
	if err != nil {
		t.Fatalf("failed to list notifications: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d", res.StatusCode)
	}

	var listResult domain.ListResult
	_ = json.NewDecoder(res.Body).Decode(&listResult)

	if listResult.Total < 1 {
		t.Fatalf("expected at least 1 notification in list result, got %d", listResult.Total)
	}
}

func TestHTTP_ValidationErrors(t *testing.T) {
	ts, svc := setupTestServer()
	defer ts.Close()
	defer svc.Stop()

	client := ts.Client()

	// Invalid Channel
	payload := map[string]interface{}{
		"channel":    "unsupported-channel",
		"recipients": []string{"user@example.com"},
		"body":       "Body",
	}
	bodyBytes, _ := json.Marshal(payload)
	res, _ := client.Post(ts.URL+"/api/v1/notifications", "application/json", bytes.NewBuffer(bodyBytes))

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for invalid channel, got %d", res.StatusCode)
	}
}
