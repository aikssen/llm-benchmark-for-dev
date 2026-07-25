package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evercifuentes/notifier/internal/httpapi"
	"github.com/evercifuentes/notifier/internal/platform/log"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/provider/simulated"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/service"
	"github.com/evercifuentes/notifier/internal/store/memory"
)

// stubHealth reports a fixed readiness verdict.
type stubHealth struct{ err error }

func (s stubHealth) Ready() error { return s.err }

func (s stubHealth) Details() map[string]any { return map[string]any{"workers": 4} }

func newServer(t *testing.T, queueCapacity int, health httpapi.HealthChecker) http.Handler {
	t.Helper()

	registry := provider.NewRegistry()
	registry.MustRegister(
		simulated.NewEmail(simulated.Behaviour{}),
		simulated.NewSMS(simulated.Behaviour{}),
		simulated.NewPush(simulated.Behaviour{}),
	)
	svc := service.New(memory.New(), queue.New(queueCapacity), registry,
		service.DefaultLimits(), log.Discard())

	return httpapi.NewRouter(svc, health, httpapi.DefaultConfig(), log.Discard())
}

// do performs a request against the handler and returns the recorder.
func do(t *testing.T, h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals a JSON response body.
func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return v
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Field     string `json:"field"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

type notificationBody struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	Status  string `json:"status"`
	Summary struct {
		Total   int `json:"total"`
		Pending int `json:"pending"`
	} `json:"summary"`
	Deliveries []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"deliveries"`
}

const validBody = `{
	"channel": "email",
	"recipients": [{"id": "u1", "address": "a@example.com"}, {"address": "b@example.com"}],
	"message": {"subject": "Hi", "body": "Hello"}
}`

func TestSubmitReturns202WithALocationHeader(t *testing.T) {
	t.Parallel()

	h := newServer(t, 100, stubHealth{})
	rec := do(t, h, http.MethodPost, "/v1/notifications", validBody, nil)

	// 202, not 201: the notification is accepted, delivery has not happened.
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusAccepted, rec.Body)
	}

	body := decode[notificationBody](t, rec)
	if body.ID == "" {
		t.Error("response has no notification id")
	}
	if body.Summary.Total != 2 || len(body.Deliveries) != 2 {
		t.Errorf("summary = %+v with %d deliveries, want 2", body.Summary, len(body.Deliveries))
	}
	if body.Status != "pending" {
		t.Errorf("status = %q, want %q", body.Status, "pending")
	}
	if got, want := rec.Header().Get("Location"), "/v1/notifications/"+body.ID; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("response carries no X-Request-ID for correlation")
	}
}

func TestSubmitRejectsBadRequests(t *testing.T) {
	t.Parallel()

	h := newServer(t, 100, stubHealth{})

	tests := []struct {
		name      string
		body      string
		wantCode  string
		wantField string
	}{
		{
			name:     "empty body",
			body:     "",
			wantCode: "malformed_json",
		},
		{
			name:     "invalid json",
			body:     `{"channel":`,
			wantCode: "malformed_json",
		},
		{
			// A misspelled field would otherwise silently send the wrong
			// notification.
			name:     "unknown field",
			body:     `{"channel":"email","recipeints":[]}`,
			wantCode: "malformed_json",
		},
		{
			name:     "wrong type",
			body:     `{"channel": 42}`,
			wantCode: "malformed_json",
		},
		{
			name:     "two json objects",
			body:     `{"channel":"email"}{"channel":"sms"}`,
			wantCode: "malformed_json",
		},
		{
			name:      "unsupported channel",
			body:      `{"channel":"fax","recipients":[{"address":"a@example.com"}],"message":{"body":"Hi"}}`,
			wantCode:  "invalid_request",
			wantField: "channel",
		},
		{
			name:      "no recipients",
			body:      `{"channel":"email","recipients":[],"message":{"body":"Hi"}}`,
			wantCode:  "invalid_request",
			wantField: "recipients",
		},
		{
			name:      "malformed address",
			body:      `{"channel":"sms","recipients":[{"address":"not-a-phone"}],"message":{"body":"Hi"}}`,
			wantCode:  "invalid_request",
			wantField: "recipients[0].address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := do(t, h, http.MethodPost, "/v1/notifications", tt.body, nil)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}
			env := decode[errorEnvelope](t, rec)
			if env.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if tt.wantField != "" && env.Error.Field != tt.wantField {
				t.Errorf("field = %q, want %q", env.Error.Field, tt.wantField)
			}
			if env.Error.RequestID == "" {
				t.Error("the error carries no request id")
			}
		})
	}
}

func TestSubmitHonoursTheIdempotencyKeyHeader(t *testing.T) {
	t.Parallel()

	h := newServer(t, 100, stubHealth{})
	headers := map[string]string{"Idempotency-Key": "key-1"}

	first := decode[notificationBody](t, do(t, h, http.MethodPost, "/v1/notifications", validBody, headers))
	second := decode[notificationBody](t, do(t, h, http.MethodPost, "/v1/notifications", validBody, headers))

	if first.ID != second.ID {
		t.Errorf("replay created %q, want the original %q", second.ID, first.ID)
	}
}

// Backpressure must be visible to the client, with a hint about when to
// retry, rather than surfacing as a timeout.
func TestSubmitReturns429WhenTheQueueIsFull(t *testing.T) {
	t.Parallel()

	h := newServer(t, 2, stubHealth{})

	// The first request fills the 2-slot queue.
	if rec := do(t, h, http.MethodPost, "/v1/notifications", validBody, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("first submit status = %d, want 202", rec.Code)
	}
	rec := do(t, h, http.MethodPost, "/v1/notifications", validBody, nil)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body: %s)", rec.Code, rec.Body)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 must tell the client when to retry")
	}
	if env := decode[errorEnvelope](t, rec); env.Error.Code != "service_overloaded" {
		t.Errorf("code = %q, want %q", env.Error.Code, "service_overloaded")
	}
}

func TestGetNotification(t *testing.T) {
	t.Parallel()

	h := newServer(t, 100, stubHealth{})
	created := decode[notificationBody](t, do(t, h, http.MethodPost, "/v1/notifications", validBody, nil))

	rec := do(t, h, http.MethodGet, "/v1/notifications/"+created.ID, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	if got := decode[notificationBody](t, rec); got.ID != created.ID {
		t.Errorf("returned %q, want %q", got.ID, created.ID)
	}
}

func TestGetUnknownNotificationReturns404(t *testing.T) {
	t.Parallel()

	h := newServer(t, 100, stubHealth{})
	rec := do(t, h, http.MethodGet, "/v1/notifications/ntf_missing", "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if env := decode[errorEnvelope](t, rec); env.Error.Code != "not_found" {
		t.Errorf("code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestListNotifications(t *testing.T) {
	t.Parallel()

	h := newServer(t, 200, stubHealth{})
	for i := range 3 {
		body := fmt.Sprintf(
			`{"channel":"email","recipients":[{"address":"user%d@example.com"}],"message":{"subject":"Hi","body":"Hello"}}`, i)
		if rec := do(t, h, http.MethodPost, "/v1/notifications", body, nil); rec.Code != http.StatusAccepted {
			t.Fatalf("seeding failed with status %d: %s", rec.Code, rec.Body)
		}
	}

	type listBody struct {
		Items      []notificationBody `json:"items"`
		Pagination struct {
			Total  int `json:"total"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		} `json:"pagination"`
	}

	t.Run("returns every notification with pagination metadata", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/v1/notifications", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		got := decode[listBody](t, rec)
		if got.Pagination.Total != 3 || len(got.Items) != 3 {
			t.Errorf("returned %d of %d items, want 3 of 3", len(got.Items), got.Pagination.Total)
		}
	})

	t.Run("honours limit and offset", func(t *testing.T) {
		got := decode[listBody](t, do(t, h, http.MethodGet, "/v1/notifications?limit=2&offset=1", "", nil))
		if len(got.Items) != 2 || got.Pagination.Limit != 2 || got.Pagination.Offset != 1 {
			t.Errorf("pagination = %+v with %d items, want limit 2 offset 1", got.Pagination, len(got.Items))
		}
	})

	t.Run("filters by channel", func(t *testing.T) {
		got := decode[listBody](t, do(t, h, http.MethodGet, "/v1/notifications?channel=sms", "", nil))
		if got.Pagination.Total != 0 {
			t.Errorf("Total = %d, want 0: nothing was sent over SMS", got.Pagination.Total)
		}
		if got.Items == nil {
			t.Error("items should serialise as [] rather than null")
		}
	})

	t.Run("rejects invalid query parameters", func(t *testing.T) {
		for _, target := range []string{
			"/v1/notifications?limit=abc",
			"/v1/notifications?offset=-1",
			"/v1/notifications?channel=fax",
			"/v1/notifications?status=in_progress",
		} {
			rec := do(t, h, http.MethodGet, target, "", nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", target, rec.Code)
			}
		}
	})
}

func TestChannelsEndpointReportsWhatTheDeploymentSupports(t *testing.T) {
	t.Parallel()

	h := newServer(t, 10, stubHealth{})
	rec := do(t, h, http.MethodGet, "/v1/channels", "", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decode[struct {
		Channels []string `json:"channels"`
	}](t, rec)

	want := []string{"email", "push", "sms"}
	if len(got.Channels) != len(want) {
		t.Fatalf("channels = %v, want %v", got.Channels, want)
	}
	for i := range want {
		if got.Channels[i] != want[i] {
			t.Fatalf("channels = %v, want %v", got.Channels, want)
		}
	}
}

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("liveness is independent of readiness", func(t *testing.T) {
		h := newServer(t, 10, stubHealth{err: errors.New("still starting")})
		if rec := do(t, h, http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusOK {
			t.Errorf("healthz status = %d, want 200 even while not ready", rec.Code)
		}
	})

	t.Run("ready", func(t *testing.T) {
		h := newServer(t, 10, stubHealth{})
		rec := do(t, h, http.MethodGet, "/readyz", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("readyz status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"workers"`) {
			t.Error("readyz should include the diagnostic details")
		}
	})

	t.Run("not ready", func(t *testing.T) {
		h := newServer(t, 10, stubHealth{err: errors.New("queue is closed")})
		rec := do(t, h, http.MethodGet, "/readyz", "", nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("readyz status = %d, want 503", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "queue is closed") {
			t.Error("readyz should explain why the instance is not ready")
		}
	})
}

func TestUnknownRoutesAndMethods(t *testing.T) {
	t.Parallel()

	h := newServer(t, 10, stubHealth{})

	rec := do(t, h, http.MethodGet, "/v1/unknown", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", rec.Code)
	}
	if env := decode[errorEnvelope](t, rec); env.Error.Code != "not_found" {
		t.Errorf("code = %q, want a structured not_found error", env.Error.Code)
	}

	// The route exists but not for this method; net/http answers 405.
	if rec := do(t, h, http.MethodDelete, "/v1/notifications", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("unsupported method status = %d, want 405", rec.Code)
	}
}

// An incoming correlation id must be preserved so a trace can span services.
func TestIncomingRequestIDIsPropagated(t *testing.T) {
	t.Parallel()

	h := newServer(t, 10, stubHealth{})
	rec := do(t, h, http.MethodGet, "/v1/channels", "", map[string]string{"X-Request-ID": "trace-abc"})

	if got := rec.Header().Get("X-Request-ID"); got != "trace-abc" {
		t.Errorf("X-Request-ID = %q, want the incoming %q", got, "trace-abc")
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	registry.MustRegister(simulated.NewEmail(simulated.Behaviour{}))
	svc := service.New(memory.New(), queue.New(10), registry, service.DefaultLimits(), log.Discard())
	h := httpapi.NewRouter(svc, stubHealth{}, httpapi.Config{
		MaxRequestBytes: 128,
		RequestTimeout:  httpapi.DefaultConfig().RequestTimeout,
	}, log.Discard())

	body := fmt.Sprintf(`{"channel":"email","recipients":[{"address":"a@example.com"}],"message":{"body":%q}}`,
		strings.Repeat("x", 512))

	rec := do(t, h, http.MethodPost, "/v1/notifications", body, nil)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body: %s)", rec.Code, rec.Body)
	}
	if env := decode[errorEnvelope](t, rec); env.Error.Code != "payload_too_large" {
		t.Errorf("code = %q, want %q", env.Error.Code, "payload_too_large")
	}
}

func TestResponsesAreJSON(t *testing.T) {
	t.Parallel()

	h := newServer(t, 10, stubHealth{})
	for _, target := range []string{"/v1/channels", "/healthz", "/v1/notifications", "/v1/notifications/ntf_missing"} {
		rec := do(t, h, http.MethodGet, target, "", nil)
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("%s: Content-Type = %q, want JSON", target, got)
		}
	}
}
