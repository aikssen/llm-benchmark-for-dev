package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notificationservice/internal/domain"
	"notificationservice/internal/store"
	"notificationservice/internal/worker"
)

// fakeEnqueuer records enqueued jobs and can be forced to reject.
type fakeEnqueuer struct {
	jobs []worker.Job
	err  error
}

func (f *fakeEnqueuer) EnqueueBatch(jobs []worker.Job) error {
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, jobs...)
	return nil
}

func testConfig() Config {
	return Config{MaxRecipients: 3, MaxBodyBytes: 1 << 20, DefaultPageSize: 20, MaxPageSize: 100}
}

func newTestServer(t *testing.T, enq Enqueuer) (*httptest.Server, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	srv := httptest.NewServer(New(testConfig(), st, enq, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	return srv, st
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return v
}

func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	return decodeBody[errorResponse](t, resp).Error.Code
}

func TestCreateNotification(t *testing.T) {
	enq := &fakeEnqueuer{}
	srv, st := newTestServer(t, enq)

	resp := postJSON(t, srv.URL+"/v1/notifications", `{
		"channel": "email",
		"recipients": ["a@example.com", "b@example.com", "a@example.com"],
		"message": {"subject": "Hello", "body": "World"}
	}`)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	body := decodeBody[notificationResponse](t, resp)

	if body.ID == "" || body.Status != string(domain.StatusPending) {
		t.Errorf("unexpected response: %+v", body)
	}
	// Duplicate recipient must be collapsed.
	if len(body.Deliveries) != 2 {
		t.Fatalf("deliveries = %d, want 2 (deduplicated)", len(body.Deliveries))
	}
	if len(enq.jobs) != 2 {
		t.Errorf("enqueued jobs = %d, want 2", len(enq.jobs))
	}

	// The notification is persisted and retrievable.
	if _, err := st.GetNotification(t.Context(), body.ID); err != nil {
		t.Errorf("notification not persisted: %v", err)
	}
}

func TestCreateNotificationRejectsBadRequests(t *testing.T) {
	srv, _ := newTestServer(t, &fakeEnqueuer{})

	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"malformed json", `{`, "invalid_body"},
		{"unknown field", `{"chanel": "email"}`, "invalid_body"},
		{"unknown channel", `{"channel": "fax", "recipients": ["x"], "message": {"body": "b"}}`, "invalid_request"},
		{"no recipients", `{"channel": "email", "recipients": [], "message": {"body": "b"}}`, "invalid_request"},
		{"blank recipient", `{"channel": "email", "recipients": [" "], "message": {"body": "b"}}`, "invalid_request"},
		{"empty body", `{"channel": "email", "recipients": ["a@b.com"], "message": {"subject": "s"}}`, "invalid_request"},
		{"too many recipients", `{"channel": "sms", "recipients": ["+1","+2","+3","+4"], "message": {"body": "b"}}`, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := postJSON(t, srv.URL+"/v1/notifications", tt.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if code := errorCode(t, resp); code != tt.wantCode {
				t.Errorf("error code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}

func TestCreateNotificationBackpressure(t *testing.T) {
	enq := &fakeEnqueuer{err: fmt.Errorf("wrapped: %w", worker.ErrQueueFull)}
	srv, st := newTestServer(t, enq)

	resp := postJSON(t, srv.URL+"/v1/notifications", `{
		"channel": "sms",
		"recipients": ["+14155552671"],
		"message": {"body": "hi"}
	}`)

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 response should include Retry-After")
	}
	// The rejected notification must not linger in the store.
	res, err := st.ListNotifications(t.Context(), store.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Errorf("store has %d notifications after rejected submit, want 0", res.Total)
	}
}

func TestGetNotification(t *testing.T) {
	srv, st := newTestServer(t, &fakeEnqueuer{})

	n := domain.Notification{
		ID:      "ntf_x",
		Channel: domain.ChannelPush,
		Message: domain.Message{Body: "b"},
		Deliveries: []domain.Delivery{
			{ID: "dlv_1", Recipient: "token", Status: domain.DeliveryDelivered},
			{ID: "dlv_2", Recipient: "token2", Status: domain.DeliveryFailed, LastError: "boom"},
		},
	}
	if err := st.CreateNotification(t.Context(), n); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/v1/notifications/ntf_x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody[notificationResponse](t, resp)
	if body.Status != string(domain.StatusPartiallyDelivered) {
		t.Errorf("status = %q, want partially_delivered", body.Status)
	}
	if body.Deliveries[1].Error != "boom" {
		t.Errorf("delivery error not exposed: %+v", body.Deliveries[1])
	}

	missing, err := http.Get(srv.URL + "/v1/notifications/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("status for missing = %d, want 404", missing.StatusCode)
	}
}

func TestListNotifications(t *testing.T) {
	srv, st := newTestServer(t, &fakeEnqueuer{})

	for i := range 5 {
		ch := domain.ChannelEmail
		if i >= 3 {
			ch = domain.ChannelSMS
		}
		n := domain.Notification{
			ID:         fmt.Sprintf("ntf_%d", i),
			Channel:    ch,
			Message:    domain.Message{Body: "b"},
			Deliveries: []domain.Delivery{{ID: "d", Status: domain.DeliveryPending}},
		}
		if err := st.CreateNotification(t.Context(), n); err != nil {
			t.Fatal(err)
		}
	}

	get := func(t *testing.T, query string) (*http.Response, error) {
		t.Helper()
		return http.Get(srv.URL + "/v1/notifications" + query)
	}

	t.Run("paginates newest first", func(t *testing.T) {
		resp, err := get(t, "?limit=2&offset=1")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body := decodeBody[listResponse](t, resp)
		if body.Total != 5 || len(body.Items) != 2 {
			t.Fatalf("total = %d, items = %d, want 5/2", body.Total, len(body.Items))
		}
		if body.Items[0].ID != "ntf_3" {
			t.Errorf("first item = %q, want ntf_3", body.Items[0].ID)
		}
	})

	t.Run("filters by channel", func(t *testing.T) {
		resp, err := get(t, "?channel=sms")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if body := decodeBody[listResponse](t, resp); body.Total != 2 {
			t.Errorf("total = %d, want 2", body.Total)
		}
	})

	t.Run("rejects bad query params", func(t *testing.T) {
		for _, q := range []string{"?limit=0", "?limit=9999", "?offset=-1", "?channel=fax", "?status=weird"} {
			resp, err := get(t, q)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("query %q: status = %d, want 400", q, resp.StatusCode)
			}
		}
	})
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t, &fakeEnqueuer{})
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
