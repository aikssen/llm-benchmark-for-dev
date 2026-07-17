package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anomalyco/notification-service/internal/application"
	"github.com/anomalyco/notification-service/internal/application/validation"
	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/httpapi"
	"github.com/anomalyco/notification-service/internal/infrastructure/providers"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

type okProvider struct{}

func (okProvider) Name() string { return "ok" }
func (okProvider) Channels() []domain.Channel {
	return []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush}
}
func (okProvider) Send(_ context.Context, _ *domain.Notification) error { return nil }

type slowProvider struct{}

func (slowProvider) Name() string               { return "slow" }
func (slowProvider) Channels() []domain.Channel { return []domain.Channel{domain.ChannelEmail} }
func (slowProvider) Send(ctx context.Context, _ *domain.Notification) error {
	select {
	case <-time.After(500 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newTestServer(t *testing.T, p providers.Provider) (*httptest.Server, *application.NotificationService, func()) {
	t.Helper()
	repo := repository.NewMemoryRepository()
	reg := providers.NewRegistry()
	reg.Register(p)
	pipe := validation.NewPipeline(validation.ContentValidator{})
	cfg := application.DefaultDispatcherConfig()
	cfg.WorkerCount = 2
	cfg.BaseBackoff = time.Millisecond
	cfg.MaxBackoff = 2 * time.Millisecond
	cfg.PerAttemptTimeout = 50 * time.Millisecond
	cfg.MaxAttempts = 2
	log := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	d := application.NewDispatcher(cfg, repo, reg, pipe, log)
	svc := application.NewNotificationService(repo, d, log)
	d.Start(context.Background())
	h := httpapi.NewHandler(svc)
	r := httpapi.NewRouter(h, log)
	srv := httptest.NewServer(r.Handler())
	return srv, svc, func() { srv.Close(); d.Shutdown() }
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func TestHTTP_SubmitAndGet(t *testing.T) {
	srv, svc, cleanup := newTestServer(t, okProvider{})
	defer cleanup()

	body := map[string]any{
		"recipients": []string{"a@b.com"},
		"channel":    "email",
		"message":    map[string]any{"subject": "hi", "body": "hello"},
	}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/notifications", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Notifications []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"notifications"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Notifications) != 1 {
		t.Fatalf("expected 1, got %d", len(got.Notifications))
	}

	deadline := time.Now().Add(2 * time.Second)
	var final *domain.Notification
	for time.Now().Before(deadline) {
		final, _ = svc.Get(context.Background(), got.Notifications[0].ID)
		if final != nil && final.Status.Terminal() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final == nil || final.Status != domain.StatusDelivered {
		t.Errorf("expected delivered, got %+v", final)
	}

	resp2, err := http.Get(srv.URL + "/v1/notifications/" + got.Notifications[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get status = %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestHTTP_InvalidChannel(t *testing.T) {
	srv, _, cleanup := newTestServer(t, okProvider{})
	defer cleanup()
	body := map[string]any{
		"recipients": []string{"a@b.com"},
		"channel":    "fax",
		"message":    map[string]any{"body": "x"},
	}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/notifications", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHTTP_ListPagination(t *testing.T) {
	srv, _, cleanup := newTestServer(t, okProvider{})
	defer cleanup()
	for i := 0; i < 3; i++ {
		body := map[string]any{
			"recipients": []string{"a@b.com"},
			"channel":    "email",
			"message":    map[string]any{"subject": "s", "body": "x"},
		}
		buf, _ := json.Marshal(body)
		_, err := http.Post(srv.URL+"/v1/notifications", "application/json", bytes.NewReader(buf))
		if err != nil {
			t.Fatal(err)
		}
	}
	resp, err := http.Get(srv.URL + "/v1/notifications?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Errorf("expected count=2, got %d", out.Count)
	}
}

func TestHTTP_NotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t, okProvider{})
	defer cleanup()
	resp, err := http.Get(srv.URL + "/v1/notifications/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHTTP_Healthz(t *testing.T) {
	srv, _, cleanup := newTestServer(t, okProvider{})
	defer cleanup()
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHTTP_TimeoutReturnsError(t *testing.T) {
	srv, _, cleanup := newTestServer(t, slowProvider{})
	defer cleanup()
	body := map[string]any{
		"recipients": []string{"a@b.com"},
		"channel":    "email",
		"message":    map[string]any{"subject": "s", "body": "x"},
	}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/notifications", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Notifications []struct {
			ID string `json:"id"`
		} `json:"notifications"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Notifications) != 1 {
		t.Fatal("expected 1 notification")
	}
	deadline := time.Now().Add(2 * time.Second)
	var n *domain.Notification
	for time.Now().Before(deadline) {
		getResp, _ := http.Get(srv.URL + "/v1/notifications/" + got.Notifications[0].ID)
		var cur domain.Notification
		_ = json.NewDecoder(getResp.Body).Decode(&cur)
		getResp.Body.Close()
		if cur.Status.Terminal() {
			nc := cur
			n = &nc
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if n == nil {
		t.Fatal("notification never reached terminal state")
	}
	if n.Status == domain.StatusDelivered {
		t.Errorf("expected failure due to timeout, got delivered")
	}
}
