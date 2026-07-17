package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-orchestrator/internal/domain"
	"github.com/example/notification-orchestrator/internal/provider"
	"github.com/example/notification-orchestrator/internal/service"
	"github.com/example/notification-orchestrator/internal/store"
	"github.com/example/notification-orchestrator/internal/transport/httpapi"
	"github.com/example/notification-orchestrator/internal/validation"
)

func TestCreateAndGetNotification(t *testing.T) {
	repository := store.NewMemoryRepository()
	dispatcher := service.New(repository,
		provider.NewRegistry(map[domain.Channel]provider.Provider{domain.ChannelEmail: successProvider{}}),
		validation.NewPipeline(1, validation.RecipientFormat{}, validation.MessageContent{MaxBytes: 1000}),
		service.Config{QueueCapacity: 2, NotificationWorkers: 1, RecipientWorkers: 1, MaxRecipients: 10, MaxAttempts: 1, AttemptTimeout: time.Second, RetryBaseDelay: time.Millisecond},
		log.New(io.Discard, "", 0))
	dispatcher.Start()
	t.Cleanup(func() { _ = dispatcher.Stop(context.Background()) })
	handler := httpapi.NewHandler(dispatcher, log.New(io.Discard, "", 0))

	create := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewBufferString(`{"recipients":["person@example.com"],"channel":"email","message":"Hello"}`))
	create.Header.Set("Idempotency-Key", "http-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, create)
	if response.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, body=%s", response.Code, response.Body.String())
	}

	var output struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/v1/notifications/"+output.ID, nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", getResponse.Code, getResponse.Body.String())
	}
}

type successProvider struct{}

func (successProvider) Send(context.Context, provider.Payload) error { return nil }
