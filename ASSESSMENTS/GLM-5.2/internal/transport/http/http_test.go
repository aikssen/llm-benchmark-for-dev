package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/notification-service/internal/channel"
	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/processing"
	"github.com/example/notification-service/internal/provider"
	"github.com/example/notification-service/internal/service"
	"github.com/example/notification-service/internal/storage"
	"github.com/example/notification-service/internal/validation"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

type alwaysAllow struct{}

func (alwaysAllow) Allows(context.Context, string, domain.Channel) (bool, error) { return true, nil }

type okProvider struct{ attempts int }

func (okProvider) Name() string                                  { return "ok" }
func (okProvider) Send(context.Context, provider.Outbound) error { return nil }

// setup builds a full in-process stack and returns the HTTP handler, the
// repository (to assert on state) and a stop function for the processor.
func setup(t *testing.T) (http.Handler, *storage.Memory, func()) {
	t.Helper()
	repo := storage.NewMemory()
	queue := processing.NewQueue(8)
	pref := &alwaysAllow{}
	limiter := validation.NewSlidingWindowLimiter(time.Hour, 1000)
	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(),
		validation.NewPreferenceValidator(pref),
		validation.NewRateLimitValidator(limiter),
	)
	channels := channel.NewRegistry().
		Register(channel.NewEmail(provider.WithAttempts()(okProvider{}))).
		Register(channel.NewSMS(provider.WithAttempts()(okProvider{}))).
		Register(channel.NewPush(provider.WithAttempts()(okProvider{})))

	proc := processing.New(processing.Config{Workers: 2, RecipientConcurrency: 4},
		queue, repo, channels, pipeline, quietLogger())
	proc.Start(context.Background())

	svc := service.New(repo, queue, []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush})
	router := NewRouter(svc, quietLogger(), 20, 100)
	return router, repo, proc.Stop
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCreateAndGetNotification(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()

	body := map[string]any{
		"channel":    "email",
		"recipients": []map[string]string{{"id": "u1", "email": "a@b.com"}, {"id": "u2", "email": "c@d.com"}},
		"message":    map[string]any{"subject": "Hi", "body": "Hello"},
	}
	w := do(t, h, http.MethodPost, "/api/v1/notifications", body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp notificationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" || resp.Status != string(domain.StatusAccepted) {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, resp.ID) {
		t.Fatalf("location header should contain id, got %q", loc)
	}

	// Wait for processing to reach a terminal state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gw := do(t, h, http.MethodGet, "/api/v1/notifications/"+resp.ID, nil)
		var gr notificationResponse
		_ = json.NewDecoder(gw.Body).Decode(&gr)
		if gr.Status == string(domain.StatusDelivered) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	gw := do(t, h, http.MethodGet, "/api/v1/notifications/"+resp.ID, nil)
	if gw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", gw.Code)
	}
	var gr notificationResponse
	if err := json.NewDecoder(gw.Body).Decode(&gr); err != nil {
		t.Fatal(err)
	}
	if gr.Status != string(domain.StatusDelivered) {
		t.Fatalf("expected delivered, got %s", gr.Status)
	}
	if len(gr.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(gr.Results))
	}
}

func TestGetMissingReturns404(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()
	w := do(t, h, http.MethodGet, "/api/v1/notifications/does-not-exist", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateUnsupportedChannel(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()
	body := map[string]any{"channel": "carrier-pigeon", "recipients": []map[string]string{{"id": "u1"}}, "message": map[string]any{"body": "x"}}
	w := do(t, h, http.MethodPost, "/api/v1/notifications", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader([]byte("{not json")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateValidationErrorsReported(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()
	// Email channel requires subject and valid email; this fails both
	// content and recipient validators.
	body := map[string]any{
		"channel":    "email",
		"recipients": []map[string]string{{"id": "u1", "email": "bad"}},
		"message":    map[string]any{"body": "B"},
	}
	w := do(t, h, http.MethodPost, "/api/v1/notifications", body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("create should still be accepted (202), got %d: %s", w.Code, w.Body.String())
	}
	var resp notificationResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// Wait for terminal (partial/failed).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gw := do(t, h, http.MethodGet, "/api/v1/notifications/"+resp.ID, nil)
		_ = json.NewDecoder(gw.Body).Decode(&resp)
		if resp.Status == string(domain.StatusFailed) || resp.Status == string(domain.StatusPartial) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp.Status != string(domain.StatusFailed) {
		t.Fatalf("expected failed, got %s", resp.Status)
	}
	found := false
	for _, r := range resp.Results {
		if r.RecipientID == "u1" && r.Status == string(domain.StatusValidationFailed) && len(r.ValidationErrors) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected validation_failed result with errors, got %+v", resp.Results)
	}
}

func TestListNotifications(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()
	for i := 0; i < 3; i++ {
		body := map[string]any{
			"channel":    "sms",
			"recipients": []map[string]string{{"id": "u1", "phone": "+14155550123"}},
			"message":    map[string]any{"body": "B"},
		}
		w := do(t, h, http.MethodPost, "/api/v1/notifications", body)
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", w.Code)
		}
	}
	w := do(t, h, http.MethodGet, "/api/v1/notifications?limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var lr listResponse
	if err := json.NewDecoder(w.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}
	if lr.Total != 3 || len(lr.Notifications) != 2 {
		t.Fatalf("expected total=3 len=2, got total=%d len=%d", lr.Total, len(lr.Notifications))
	}
}

func TestHealth(t *testing.T) {
	h, _, stop := setup(t)
	defer stop()
	w := do(t, h, http.MethodGet, "/api/v1/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestPanicRecovery(t *testing.T) {
	// Build a router with a handler that panics.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	r := chain(mux, requestIDMiddleware, recoveryMiddleware(quietLogger()))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic recovery, got %d", w.Code)
	}
}
