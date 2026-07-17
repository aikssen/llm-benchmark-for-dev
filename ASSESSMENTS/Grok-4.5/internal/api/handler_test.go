package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
	"github.com/evercifuentes/notifications/internal/provider"
	"github.com/evercifuentes/notifications/internal/service"
	"github.com/evercifuentes/notifications/internal/store"
	"github.com/evercifuentes/notifications/internal/validation"
	"github.com/evercifuentes/notifications/internal/worker"
)

func newTestServer(t *testing.T) (*http.ServeMux, *service.Service) {
	t.Helper()
	st := store.NewMemoryStore()
	prefs := validation.NewMemoryPreferences()
	limiter := validation.NewSlidingWindowLimiter(1000, time.Minute)
	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(2000),
		validation.NewPreferencesValidator(prefs),
		validation.NewRateLimitValidator(limiter),
	)
	registry := provider.NewRegistry()
	registry.Register(provider.NewSimulatedProvider(domain.ChannelEmail, 0, 0, 0))
	registry.Register(provider.NewSimulatedProvider(domain.ChannelSMS, 0, 0, 0))
	registry.Register(provider.NewSimulatedProvider(domain.ChannelPush, 0, 0, 0))

	ctx, cancel := context.WithCancel(context.Background())
	pool := worker.NewPool(st, pipeline, registry, worker.Config{
		Workers:    2,
		QueueSize:  32,
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	})
	pool.Start(ctx)
	t.Cleanup(func() {
		cancel()
		pool.Stop()
	})

	svc := service.New(st, pool)
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, svc
}

func TestHealthz(t *testing.T) {
	mux, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestCreateAndGetNotification(t *testing.T) {
	mux, _ := newTestServer(t)

	body := map[string]any{
		"recipients": []string{"user@example.com"},
		"channel":    "email",
		"subject":    "Test",
		"body":       "Hello",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var created domain.CreateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.NotificationIDs) != 1 {
		t.Fatalf("ids=%v", created.NotificationIDs)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/notifications/"+created.NotificationIDs[0], nil)
	getRR := httptest.NewRecorder()
	mux.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	mux, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader([]byte(`{`)))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestListNotifications(t *testing.T) {
	mux, _ := newTestServer(t)

	body := `{"recipients":["user@example.com"],"channel":"email","subject":"s","body":"b"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader([]byte(body)))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("create status=%d", rr.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/notifications?limit=10", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRR.Code, listRR.Body.String())
	}
}

func TestGetNotFound(t *testing.T) {
	mux, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/notifications/does-not-exist", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}
