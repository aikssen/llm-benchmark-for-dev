package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
)

// fakeNotifier implements the Notifier interface for transport-level tests.
type fakeNotifier struct {
	submitFn func(context.Context, service.SubmitRequest) ([]*domain.Notification, error)
	getFn    func(context.Context, string) (*domain.Notification, error)
	listFn   func(context.Context, store.ListFilter) ([]*domain.Notification, int, error)
}

func (f fakeNotifier) Submit(ctx context.Context, r service.SubmitRequest) ([]*domain.Notification, error) {
	return f.submitFn(ctx, r)
}
func (f fakeNotifier) Get(ctx context.Context, id string) (*domain.Notification, error) {
	return f.getFn(ctx, id)
}
func (f fakeNotifier) List(ctx context.Context, fl store.ListFilter) ([]*domain.Notification, int, error) {
	return f.listFn(ctx, fl)
}

func testHandler(n Notifier) http.Handler {
	return NewServer(n, nil).Handler()
}

func TestHandleSubmit_Accepted(t *testing.T) {
	var captured service.SubmitRequest
	h := testHandler(fakeNotifier{
		submitFn: func(_ context.Context, r service.SubmitRequest) ([]*domain.Notification, error) {
			captured = r
			return []*domain.Notification{
				{ID: "n1", RequestID: "r1", Channel: r.Channel, Recipient: r.Recipients[0], Status: domain.StatusPending},
			}, nil
		},
	})

	body := `{"channel":"email","recipients":["a@example.com"],"message":{"body":"hi"}}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(body)))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if captured.Channel != domain.ChannelEmail || len(captured.Recipients) != 1 {
		t.Errorf("request not mapped correctly: %+v", captured)
	}
	var resp submitResponse
	mustJSON(t, rr, &resp)
	if resp.Count != 1 || resp.RequestID != "r1" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleSubmit_ValidationError(t *testing.T) {
	h := testHandler(fakeNotifier{
		submitFn: func(context.Context, service.SubmitRequest) ([]*domain.Notification, error) {
			return nil, domain.ErrValidation
		},
	})
	rr := httptest.NewRecorder()
	body := `{"channel":"bad","recipients":["x"],"message":{"body":"hi"}}`
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleSubmit_MalformedJSON(t *testing.T) {
	h := testHandler(fakeNotifier{})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(`{bad`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleSubmit_UnknownFieldsRejected(t *testing.T) {
	h := testHandler(fakeNotifier{})
	rr := httptest.NewRecorder()
	body := `{"channel":"email","recipients":["x"],"message":{"body":"hi"},"evil":true}`
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", rr.Code)
	}
}

func TestHandleGet(t *testing.T) {
	h := testHandler(fakeNotifier{
		getFn: func(_ context.Context, id string) (*domain.Notification, error) {
			if id != "n1" {
				return nil, domain.ErrNotFound
			}
			return &domain.Notification{ID: "n1", Channel: domain.ChannelEmail, Status: domain.StatusDelivered}, nil
		},
	})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/notifications/n1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var dto notificationDTO
	mustJSON(t, rr, &dto)
	if dto.ID != "n1" || dto.Status != "delivered" {
		t.Errorf("unexpected dto: %+v", dto)
	}

	// Not found -> 404.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/notifications/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleList(t *testing.T) {
	var captured store.ListFilter
	h := testHandler(fakeNotifier{
		listFn: func(_ context.Context, f store.ListFilter) ([]*domain.Notification, int, error) {
			captured = f
			return []*domain.Notification{{ID: "n1"}}, 1, nil
		},
	})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/notifications?channel=email&status=delivered&limit=5&offset=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if captured.Channel != domain.ChannelEmail || captured.Status != domain.StatusDelivered {
		t.Errorf("filter not parsed: %+v", captured)
	}
	if captured.Limit != 5 || captured.Offset != 10 {
		t.Errorf("pagination not parsed: %+v", captured)
	}
	var resp listResponse
	mustJSON(t, rr, &resp)
	if resp.Total != 1 {
		t.Errorf("unexpected total: %d", resp.Total)
	}
}

func TestHandleList_InvalidLimit(t *testing.T) {
	h := testHandler(fakeNotifier{
		listFn: func(context.Context, store.ListFilter) ([]*domain.Notification, int, error) { return nil, 0, nil },
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/notifications?limit=abc", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	h := testHandler(fakeNotifier{})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := testHandler(fakeNotifier{})
	rr := httptest.NewRecorder()
	// DELETE is not routed; stdlib mux returns 405.
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/v1/notifications", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func mustJSON(t *testing.T, rr *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rr.Body.String())
	}
}
