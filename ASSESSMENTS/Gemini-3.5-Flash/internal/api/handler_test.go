package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/service"
	"notification-service/internal/validation"
)

func setupTestRouter() (http.Handler, repository.Repository, *service.Orchestrator) {
	repo := repository.NewInMemoryRepository()
	valEngine := validation.NewEngine()
	provRegistry := provider.NewRegistry()

	orchestrator := service.NewOrchestrator(repo, valEngine, provRegistry)
	handler := NewHandler(orchestrator, repo)
	router := NewRouter(handler)

	return router, repo, orchestrator
}

func TestSubmitNotification(t *testing.T) {
	router, _, _ := setupTestRouter()

	// 1. Success case
	reqBody := `{"recipients":["+15551234567"],"channel":"sms","content":"Hello API"}`
	req := httptest.NewRequest("POST", "/notifications", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected StatusAccepted, got %d", w.Code)
	}

	var resp service.SubmitResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.RequestID == "" || len(resp.NotificationIDs) != 1 {
		t.Errorf("invalid response content: %+v", resp)
	}

	// 2. Failure: Invalid JSON
	req = httptest.NewRequest("POST", "/notifications", bytes.NewBufferString(`{invalid json`))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected StatusBadRequest, got %d", w.Code)
	}

	// 3. Failure: Empty Content (Validation error)
	reqBodyEmpty := `{"recipients":["+15551234567"],"channel":"sms","content":""}`
	req = httptest.NewRequest("POST", "/notifications", bytes.NewBufferString(reqBodyEmpty))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected StatusBadRequest for empty content, got %d", w.Code)
	}
}

func TestGetNotification(t *testing.T) {
	router, repo, _ := setupTestRouter()
	ctx := context.Background()

	// Pre-populate repository
	n := &domain.Notification{
		ID:        "test-id",
		RequestID: "req-1",
		Recipient: "user@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Get me",
		Status:      domain.StatusPending,
	}
	_ = repo.Save(ctx, n)

	// 1. Success case
	req := httptest.NewRequest("GET", "/notifications/test-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK, got %d", w.Code)
	}

	var fetched domain.Notification
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if fetched.ID != "test-id" || fetched.Content != "Get me" {
		t.Errorf("unexpected notification content: %+v", fetched)
	}

	// 2. Not found case
	reqNotFound := httptest.NewRequest("GET", "/notifications/missing", nil)
	wNotFound := httptest.NewRecorder()
	router.ServeHTTP(wNotFound, reqNotFound)

	if wNotFound.Code != http.StatusNotFound {
		t.Errorf("expected StatusNotFound, got %d", wNotFound.Code)
	}
}

func TestListNotifications(t *testing.T) {
	router, repo, _ := setupTestRouter()
	ctx := context.Background()

	// Pre-populate repository with 2 notifications
	_ = repo.Save(ctx, &domain.Notification{
		ID:        "id-1",
		Recipient: "user1@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Msg 1",
	})
	_ = repo.Save(ctx, &domain.Notification{
		ID:        "id-2",
		Recipient: "user2@example.com",
		Channel:   domain.ChannelEmail,
		Content:   "Msg 2",
	})

	req := httptest.NewRequest("GET", "/notifications", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK, got %d", w.Code)
	}

	var list []domain.Notification
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if len(list) != 2 {
		t.Errorf("expected 2 notifications in list, got %d", len(list))
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("something went terribly wrong")
	})

	handler := recoveryMiddleware(mux)
	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()

	// Ensure the handler doesn't crash the program, but instead recovers and returns 500
	defer func() {
		if r := recover(); r != nil {
			t.Fatal("recoveryMiddleware let panic escape!")
		}
	}()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected StatusInternalServerError, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Errorf("expected error message in response body, got: %s", w.Body.String())
	}
}
