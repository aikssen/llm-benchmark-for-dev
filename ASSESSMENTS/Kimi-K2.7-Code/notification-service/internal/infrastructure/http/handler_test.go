package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notification-service/internal/application"
	httpinfra "notification-service/internal/infrastructure/http"
	"notification-service/internal/infrastructure/memory"
	"notification-service/internal/infrastructure/providers"
	"notification-service/pkg/workerpool"
)

func newTestHandler(t *testing.T) (*httpinfra.Handler, *application.Service, *memory.NotificationRepository) {
	t.Helper()
	repo := memory.NewNotificationRepository()
	reg := make(providers.Registry)
	reg.Register(providers.NewEmailProvider())
	pool := workerpool.NewPool(2, 100)
	svc := application.NewService(repo, reg, pool, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.Start(ctx)
	t.Cleanup(func() { svc.Shutdown(time.Second) })
	return httpinfra.NewHandler(svc), svc, repo
}

func TestHandler_Submit(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	router := httpinfra.NewRouter(handler)

	body, _ := json.Marshal(map[string]any{
		"channel":    "email",
		"recipients": []string{"alice@example.com"},
		"subject":    "Hello",
		"body":       "World",
	})
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "pending", resp.Status)
}

func TestHandler_Submit_BadRequest(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	router := httpinfra.NewRouter(handler)

	body, _ := json.Marshal(map[string]any{
		"channel":    "email",
		"recipients": []string{},
		"body":       "x",
	})
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_GetStatus(t *testing.T) {
	handler, svc, repo := newTestHandler(t)
	router := httpinfra.NewRouter(handler)
	ctx := context.Background()

	resp, err := svc.Submit(context.Background(), application.SubmitRequest{Channel: "email", Recipients: []string{"a@b.com"}, Subject: "s", Body: "b"})
	require.NoError(t, err)

	// wait until processed
	require.Eventually(t, func() bool {
		n, _ := repo.GetByID(ctx, resp.ID)
		return n.Status == "sent"
	}, 2*time.Second, 50*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/notifications/"+resp.ID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var n map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &n))
	assert.Equal(t, resp.ID, n["id"])
}

func TestHandler_Health(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	router := httpinfra.NewRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
