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

	"notifications/internal/dispatcher"
	"notifications/internal/domain"
	"notifications/internal/preferences"
	"notifications/internal/provider"
	"notifications/internal/ratelimit"
	"notifications/internal/store"
	"notifications/internal/validation"
)

// testEnv spins up the whole service (real dispatcher, workers, pipeline)
// with fast, deterministic providers.
type testEnv struct {
	server *httptest.Server
	store  *store.Memory
	disp   *dispatcher.Dispatcher
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st := store.NewMemory()

	reg, err := provider.NewRegistry(
		provider.Wrap(provider.NewSimulated(provider.SimulatedConfig{Channel: domain.ChannelEmail}), fastResilient()),
		provider.Wrap(provider.NewSimulated(provider.SimulatedConfig{Channel: domain.ChannelSMS}), fastResilient()),
		provider.Wrap(provider.NewSimulated(provider.SimulatedConfig{Channel: domain.ChannelPush}), fastResilient()),
	)
	if err != nil {
		t.Fatal(err)
	}

	pipe := validation.NewPipeline(
		validation.RecipientValidator{},
		validation.ContentValidator{},
		validation.PreferencesValidator{Store: preferences.NewSimulated()},
		validation.RateLimitValidator{Limiter: ratelimit.New(1000, time.Minute)},
	)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disp := dispatcher.New(st, pipe, reg, 4, 64, logger)
	disp.Start()

	srv := httptest.NewServer(NewServer(st, disp, reg, 10, logger).Handler())
	t.Cleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = disp.Stop(ctx)
	})
	return &testEnv{server: srv, store: st, disp: disp}
}

func fastResilient() provider.ResilientConfig {
	return provider.ResilientConfig{
		AttemptTimeout:   time.Second,
		MaxAttempts:      3,
		BaseDelay:        time.Millisecond,
		MaxDelay:         5 * time.Millisecond,
		BreakerThreshold: 5,
		BreakerResetWait: 50 * time.Millisecond,
	}
}

func post(t *testing.T, url string, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return resp.StatusCode, out
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return resp.StatusCode, out
}

func waitTerminal(t *testing.T, baseURL, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, body := get(t, baseURL+"/api/v1/notifications/"+id)
		if code != http.StatusOK {
			t.Fatalf("get status: %d %v", code, body)
		}
		switch domain.Status(body["status"].(string)) {
		case domain.StatusDelivered, domain.StatusRejected, domain.StatusFailed:
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("notification %s did not reach a terminal state", id)
	return nil
}

func TestEndToEndDelivery(t *testing.T) {
	env := newTestEnv(t)
	code, body := post(t, env.server.URL+"/api/v1/notifications", `{
		"channel": "email",
		"recipients": ["alice@example.com", "bob@example.com"],
		"content": {"subject": "Welcome", "body": "Hello there"}
	}`)
	if code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", code, body)
	}
	notifs, ok := body["notifications"].([]any)
	if !ok || len(notifs) != 2 {
		t.Fatalf("expected 2 notifications, got %v", body)
	}
	if body["request_id"] == "" {
		t.Fatal("expected a request_id")
	}

	for _, item := range notifs {
		id := item.(map[string]any)["id"].(string)
		final := waitTerminal(t, env.server.URL, id)
		if final["status"] != string(domain.StatusDelivered) {
			t.Fatalf("expected delivered, got %v (error: %v)", final["status"], final["last_error"])
		}
		if final["request_id"] != body["request_id"] {
			t.Fatalf("request_id mismatch: %v vs %v", final["request_id"], body["request_id"])
		}
	}
}

func TestInvalidRecipientIsRejectedAsynchronously(t *testing.T) {
	env := newTestEnv(t)
	code, body := post(t, env.server.URL+"/api/v1/notifications", `{
		"channel": "sms",
		"recipients": ["not-a-phone-number"],
		"content": {"body": "hi"}
	}`)
	if code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", code, body)
	}
	id := body["notifications"].([]any)[0].(map[string]any)["id"].(string)

	final := waitTerminal(t, env.server.URL, id)
	if final["status"] != string(domain.StatusRejected) {
		t.Fatalf("expected rejected, got %v", final["status"])
	}
	if final["last_error"] == "" || final["last_error"] == nil {
		t.Fatal("rejection should carry a reason")
	}
}

func TestOptedOutRecipientIsRejected(t *testing.T) {
	env := newTestEnv(t)
	code, body := post(t, env.server.URL+"/api/v1/notifications", `{
		"channel": "email",
		"recipients": ["optout@example.com"],
		"content": {"subject": "s", "body": "hi"}
	}`)
	if code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", code, body)
	}
	id := body["notifications"].([]any)[0].(map[string]any)["id"].(string)
	final := waitTerminal(t, env.server.URL, id)
	if final["status"] != string(domain.StatusRejected) {
		t.Fatalf("expected rejected, got %v", final["status"])
	}
}

func TestUndeliverableRecipientFailsPermanently(t *testing.T) {
	env := newTestEnv(t)
	code, body := post(t, env.server.URL+"/api/v1/notifications", `{
		"channel": "email",
		"recipients": ["undeliverable@example.com"],
		"content": {"subject": "s", "body": "hi"}
	}`)
	if code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", code, body)
	}
	id := body["notifications"].([]any)[0].(map[string]any)["id"].(string)
	final := waitTerminal(t, env.server.URL, id)
	if final["status"] != string(domain.StatusFailed) {
		t.Fatalf("expected failed, got %v", final["status"])
	}
	if final["attempts"].(float64) != 1 {
		t.Fatalf("permanent failures must not be retried; attempts=%v", final["attempts"])
	}
}

func TestCreateValidationErrors(t *testing.T) {
	env := newTestEnv(t)
	cases := []struct {
		name string
		body string
	}{
		{"bad json", `{not json`},
		{"unsupported channel", `{"channel":"fax","recipients":["1"],"content":{"body":"x"}}`},
		{"no recipients", `{"channel":"email","recipients":[],"content":{"body":"x"}}`},
		{"no body", `{"channel":"email","recipients":["a@b.com"],"content":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := post(t, env.server.URL+"/api/v1/notifications", tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %v", code, body)
			}
			if body["error"] == nil {
				t.Fatal("expected an error envelope")
			}
		})
	}
}

func TestGetNotFound(t *testing.T) {
	env := newTestEnv(t)
	code, body := get(t, env.server.URL+"/api/v1/notifications/does-not-exist")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %v", code, body)
	}
	if body["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("unexpected error body: %v", body)
	}
}

func TestListWithFilters(t *testing.T) {
	env := newTestEnv(t)

	_, emailResp := post(t, env.server.URL+"/api/v1/notifications", `{
		"channel": "email", "recipients": ["a@example.com"], "content": {"subject":"s","body":"x"}}`)
	_, smsResp := post(t, env.server.URL+"/api/v1/notifications", `{
		"channel": "sms", "recipients": ["+15550000001"], "content": {"body":"x"}}`)

	// Wait for both to reach a terminal state before filtering by status.
	emailID := emailResp["notifications"].([]any)[0].(map[string]any)["id"].(string)
	smsID := smsResp["notifications"].([]any)[0].(map[string]any)["id"].(string)
	waitTerminal(t, env.server.URL, emailID)
	waitTerminal(t, env.server.URL, smsID)

	code, body := get(t, env.server.URL+"/api/v1/notifications")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["count"].(float64) != 2 {
		t.Fatalf("expected 2 items, got %v", body["count"])
	}

	_, filtered := get(t, env.server.URL+"/api/v1/notifications?channel=sms")
	if filtered["count"].(float64) != 1 {
		t.Fatalf("channel filter failed: %v", filtered)
	}
	item := filtered["items"].([]any)[0].(map[string]any)
	if item["channel"] != "sms" {
		t.Fatalf("channel filter returned wrong item: %v", item)
	}

	// Both are delivered by now.
	_, byStatus := get(t, env.server.URL+"/api/v1/notifications?status=delivered")
	if byStatus["count"].(float64) != 2 {
		t.Fatalf("status filter failed: %v", byStatus)
	}

	// Unknown filter values are rejected.
	if code, _ := get(t, env.server.URL+"/api/v1/notifications?status=bogus"); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bogus status, got %d", code)
	}
}

func TestHealth(t *testing.T) {
	env := newTestEnv(t)
	code, body := get(t, env.server.URL+"/healthz")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("unexpected health response: %d %v", code, body)
	}
}
