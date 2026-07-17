package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"notification/internal/memory"
	"notification/internal/notification"
	"notification/internal/provider"
	"notification/internal/service"
)

func TestNotificationLifecycle(t *testing.T) {
	repository := memory.NewRepository()
	providers := provider.Registry{
		notification.ChannelEmail: provider.NewSimulated("email", 0, 0, 1),
	}
	processor := service.NewProcessor(repository, providers, nil, service.RetryPolicy{
		MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Timeout: time.Second,
	})
	svc := service.New(repository, processor, service.Config{
		Workers: 1, QueueCapacity: 10, MaxRecipients: 10, MaxBodyBytes: 100,
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	}()
	handler := New(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := []byte(`{"recipients":["user@example.com"],"channel":"email","message":{"subject":"Hello","body":"World"}}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d", response.Code)
	}
	var submitted notification.Notification
	if err := json.NewDecoder(response.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}

	var current notification.Notification
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		getRequest := httptest.NewRequest(http.MethodGet, "/v1/notifications/"+submitted.ID, nil)
		getResponse := httptest.NewRecorder()
		handler.ServeHTTP(getResponse, getRequest)
		if err := json.NewDecoder(getResponse.Body).Decode(&current); err != nil {
			t.Fatal(err)
		}
		if current.Status == notification.StatusDelivered {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if current.Status != notification.StatusDelivered || current.Deliveries[0].ProviderID == "" {
		t.Fatalf("notification = %+v", current)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/v1/notifications?limit=10&offset=0", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
}

func TestSubmitRejectsUnknownJSONFields(t *testing.T) {
	repository := memory.NewRepository()
	processor := service.NewProcessor(repository, provider.Registry{}, nil, service.RetryPolicy{})
	svc := service.New(repository, processor, service.Config{Workers: 0, QueueCapacity: 1, MaxRecipients: 1, MaxBodyBytes: 100})
	defer func() { _ = svc.Stop(context.Background()) }()
	handler := New(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	request := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewBufferString(`{"unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
