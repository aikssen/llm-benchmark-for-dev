package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/assessment/notify-service/internal/domain"
	"github.com/assessment/notify-service/internal/orchestrator"
	"github.com/assessment/notify-service/internal/provider"
	"github.com/assessment/notify-service/internal/store"
	"github.com/assessment/notify-service/internal/validator"
)

func setupTestHandler() *Handler {
	st := store.NewMemoryStore()
	reg := provider.NewRegistry()
	reg.Register(provider.NewEmailProvider())

	pipe := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
	)

	orch := orchestrator.New(orchestrator.Config{
		MaxWorkers:    2,
		MaxRetries:    1,
		RetryBackoff:  10 * time.Millisecond,
		RequestBuffer: 10,
	}, st, reg, pipe)

	return NewHandler(orch, st)
}

func TestSubmitNotificationInvalidBody(t *testing.T) {
	h := setupTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.SubmitNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSubmitNotificationValid(t *testing.T) {
	h := setupTestHandler()

	body := SubmitRequest{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "user@test.com"}},
		Message:    domain.Message{Body: "Hello World", Subject: "Test"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.SubmitNotification(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp SubmitResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if resp.Status != domain.StatusPending {
		t.Errorf("expected status pending, got %s", resp.Status)
	}
}

func TestSubmitNotificationValidationError(t *testing.T) {
	h := setupTestHandler()

	body := SubmitRequest{
		Channel:    domain.Channel("invalid"),
		Recipients: []domain.Recipient{{ID: "user@test.com"}},
		Message:    domain.Message{Body: "Hello"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.SubmitNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetNotificationNotFound(t *testing.T) {
	h := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	h.GetNotification(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetNotificationFound(t *testing.T) {
	h := setupTestHandler()

	// Submit first
	body := SubmitRequest{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{{ID: "user@test.com"}},
		Message:    domain.Message{Body: "Hello"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SubmitNotification(w, req)

	var submitResp SubmitResponse
	json.NewDecoder(w.Body).Decode(&submitResp)

	time.Sleep(200 * time.Millisecond)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/notifications/"+submitResp.ID, nil)
	req.SetPathValue("id", submitResp.ID)
	w = httptest.NewRecorder()

	h.GetNotification(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListNotifications(t *testing.T) {
	h := setupTestHandler()

	// Submit some
	for i := 0; i < 3; i++ {
		body := SubmitRequest{
			Channel:    domain.ChannelEmail,
			Recipients: []domain.Recipient{{ID: "user@test.com"}},
			Message:    domain.Message{Body: "Test " + string(rune('0'+i))},
		}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SubmitNotification(w, req)
	}

	time.Sleep(200 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?limit=10", nil)
	w := httptest.NewRecorder()

	h.ListNotifications(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp ListResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Total < 3 {
		t.Errorf("expected at least 3 notifications, got %d", resp.Total)
	}
}

func TestListNotificationsDefaultLimit(t *testing.T) {
	h := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	w := httptest.NewRecorder()

	h.ListNotifications(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp ListResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Limit != 20 {
		t.Errorf("expected default limit 20, got %d", resp.Limit)
	}
}
