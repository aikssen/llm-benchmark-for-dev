package api_test

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aikssen/notification-service/internal/api"
	"github.com/aikssen/notification-service/internal/delivery"
	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
	"github.com/aikssen/notification-service/internal/validation"
)

// newTestServer wires the full stack (validation, in-memory store,
// simulated providers, worker pool) the same way main.go does, so these
// tests exercise the real request path end-to-end through HTTP. Provider
// behavior is parameterized so both the happy path and exhausted-retries
// path can be exercised deterministically and quickly.
func newTestServer(t *testing.T, providerFailureRate float64, maxAttempts int) (*httptest.Server, func()) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := store.NewMemoryRepository()

	pipeline := validation.NewPipeline(
		[]validation.RequestValidator{validation.ContentValidator{}},
		[]validation.RecipientValidator{
			validation.FormatValidator{},
			validation.PreferenceValidator{Store: validation.NewInMemoryPreferenceStore()},
			validation.NewRateLimitValidator(1000),
		},
	)

	registry := provider.NewRegistry(
		provider.NewSimulated(domain.ChannelEmail, providerFailureRate, 0, time.Millisecond),
		provider.NewSimulated(domain.ChannelSMS, providerFailureRate, 0, time.Millisecond),
		provider.NewSimulated(domain.ChannelPush, providerFailureRate, 0, time.Millisecond),
	)

	dispatcher := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts:             maxAttempts,
		Policy:                  delivery.BackoffPolicy{Base: time.Millisecond, Max: 5 * time.Millisecond},
		ProviderTimeout:         time.Second,
		BreakerFailureThreshold: 1000,
		BreakerOpenDuration:     time.Second,
	}, logger)

	pool := delivery.NewWorkerPool(4, 100, dispatcher, logger)
	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)

	svc := service.New(repo, pipeline, pool, logger)
	handler := api.NewHandler(svc, logger)
	srv := httptest.NewServer(api.NewRouter(handler))

	cleanup := func() {
		srv.Close()
		cancel()
	}
	return srv, cleanup
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
}

func TestAPI_SubmitAndEventuallyDeliver(t *testing.T) {
	srv, cleanup := newTestServer(t, 0 /* never fails */, 2)
	defer cleanup()

	resp := postJSON(t, srv, "/v1/notifications", api.SubmitRequest{
		Channel:    "email",
		Recipients: []string{"a@example.com", "b@example.com"},
		Message:    api.MessageDTO{Subject: "Hi", Body: "hello"},
	})
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	submitResp := decode[api.SubmitResponse](t, resp)
	require.NotEmpty(t, submitResp.BatchID)
	require.Len(t, submitResp.Notifications, 2)

	for _, n := range submitResp.Notifications {
		assert.NotEqual(t, "failed", n.Status)
		id := n.ID
		require.Eventually(t, func() bool {
			r, err := http.Get(srv.URL + "/v1/notifications/" + id)
			require.NoError(t, err)
			got := decode[api.NotificationDTO](t, r)
			return got.Status == "delivered"
		}, time.Second, 5*time.Millisecond)
	}

	listResp, err := http.Get(srv.URL + "/v1/notifications?batch_id=" + submitResp.BatchID)
	require.NoError(t, err)
	list := decode[api.ListResponse](t, listResp)
	assert.Equal(t, 2, list.Total)
}

func TestAPI_SubmitExhaustsRetriesAndFails(t *testing.T) {
	srv, cleanup := newTestServer(t, 1 /* always fails */, 2)
	defer cleanup()

	resp := postJSON(t, srv, "/v1/notifications", api.SubmitRequest{
		Channel:    "sms",
		Recipients: []string{"+14155552671"},
		Message:    api.MessageDTO{Body: "hello"},
	})
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	submitResp := decode[api.SubmitResponse](t, resp)
	id := submitResp.Notifications[0].ID

	require.Eventually(t, func() bool {
		r, err := http.Get(srv.URL + "/v1/notifications/" + id)
		require.NoError(t, err)
		got := decode[api.NotificationDTO](t, r)
		return got.Status == "failed" && got.Attempts == 2
	}, time.Second, 5*time.Millisecond)
}

func TestAPI_RejectsUnsupportedChannel(t *testing.T) {
	srv, cleanup := newTestServer(t, 0, 1)
	defer cleanup()

	resp := postJSON(t, srv, "/v1/notifications", api.SubmitRequest{
		Channel:    "carrier-pigeon",
		Recipients: []string{"a@example.com"},
		Message:    api.MessageDTO{Body: "hello"},
	})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAPI_RejectsEmptyBody(t *testing.T) {
	srv, cleanup := newTestServer(t, 0, 1)
	defer cleanup()

	resp := postJSON(t, srv, "/v1/notifications", api.SubmitRequest{
		Channel:    "sms",
		Recipients: []string{"+14155552671"},
		Message:    api.MessageDTO{Body: ""},
	})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAPI_MixedBatchInvalidRecipientDoesNotBlockValidOnes(t *testing.T) {
	srv, cleanup := newTestServer(t, 0, 2)
	defer cleanup()

	resp := postJSON(t, srv, "/v1/notifications", api.SubmitRequest{
		Channel:    "email",
		Recipients: []string{"good@example.com", "not-an-email"},
		Message:    api.MessageDTO{Subject: "Hi", Body: "hello"},
	})
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	submitResp := decode[api.SubmitResponse](t, resp)
	require.Len(t, submitResp.Notifications, 2)

	var sawFailed, sawOK bool
	for _, n := range submitResp.Notifications {
		if n.Recipient == "not-an-email" {
			assert.Equal(t, "failed", n.Status)
			sawFailed = true
		} else {
			assert.NotEqual(t, "failed", n.Status)
			sawOK = true
		}
	}
	assert.True(t, sawFailed)
	assert.True(t, sawOK)
}

func TestAPI_GetUnknownNotificationReturns404(t *testing.T) {
	srv, cleanup := newTestServer(t, 0, 1)
	defer cleanup()

	resp, err := http.Get(srv.URL + "/v1/notifications/does-not-exist")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAPI_Health(t *testing.T) {
	srv, cleanup := newTestServer(t, 0, 1)
	defer cleanup()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
