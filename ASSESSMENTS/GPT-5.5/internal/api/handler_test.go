package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notification-service/internal/app"
	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/store"
)

func TestCreateNotification(t *testing.T) {
	repository := store.NewMemoryStore()
	queue := &apiQueue{}
	registry := provider.NewRegistry()
	registry.Register(domain.ChannelEmail, apiSender{name: "email"})
	service := app.NewService(repository, queue, registry)
	handler := NewHandler(service)

	body := `{
		"recipients": ["user@example.com"],
		"channel": "email",
		"message": {"subject": "Hello", "body": "Body"}
	}`
	request := httptest.NewRequest(http.MethodPost, "/notifications", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}

	var decoded submitResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.RequestID == "" {
		t.Fatal("request id is empty")
	}
	if len(decoded.Notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(decoded.Notifications))
	}
	if len(queue.ids) != 1 {
		t.Fatalf("enqueued ids = %d, want 1", len(queue.ids))
	}
}

func TestGetNotificationNotFound(t *testing.T) {
	repository := store.NewMemoryStore()
	registry := provider.NewRegistry()
	service := app.NewService(repository, &apiQueue{}, registry)
	handler := NewHandler(service)

	request := httptest.NewRequest(http.MethodGet, "/notifications/missing", nil)
	response := httptest.NewRecorder()

	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

type apiQueue struct {
	ids []string
}

func (q *apiQueue) Enqueue(context.Context, string) error {
	panic("service should use EnqueueBatch when available")
}

func (q *apiQueue) EnqueueBatch(_ context.Context, ids []string) error {
	q.ids = append(q.ids, ids...)
	return nil
}

type apiSender struct {
	name string
}

func (s apiSender) Name() string {
	return s.name
}

func (apiSender) Send(context.Context, domain.Notification) error {
	return nil
}
