package app_test

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

	"github.com/aikssen/notification-service/internal/app"
	"github.com/aikssen/notification-service/internal/config"
	"github.com/aikssen/notification-service/internal/domain"
)

// deterministicConfig builds an app that never randomly fails and responds
// instantly, so end-to-end assertions on final state are stable.
func deterministicConfig() config.Config {
	c := config.Default()
	c.Workers = 4
	c.QueueSize = 128
	c.ProviderFailureRate = 0
	c.ProviderPermanentRate = 0
	c.ProviderMinLatency = 0
	c.ProviderMaxLatency = 0
	c.RateLimit = 0 // disable rate limiting for the happy-path test
	return c
}

// TestEndToEnd_SubmitPollDeliver exercises the full stack: HTTP submit -> queue
// -> worker -> validation -> delivery -> status update -> HTTP get/list.
func TestEndToEnd_SubmitPollDeliver(t *testing.T) {
	application := app.Build(deterministicConfig(), discardLogger())
	application.Pool.Start(t.Context())
	defer application.Pool.Shutdown(context.Background())

	srv := httptest.NewServer(application.Handler)
	defer srv.Close()

	// Submit a request targeting three recipients.
	body := `{"channel":"email","recipients":["a@example.com","b@example.com","c@example.com"],"message":{"subject":"hi","body":"hello"}}`
	resp := post(t, srv.URL+"/v1/notifications", body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	var submit struct {
		Count         int `json:"count"`
		Notifications []struct {
			ID string `json:"id"`
		} `json:"notifications"`
	}
	decode(t, resp, &submit)
	if submit.Count != 3 {
		t.Fatalf("expected 3 notifications, got %d", submit.Count)
	}

	// Poll each until it reaches a terminal state.
	for _, n := range submit.Notifications {
		status := pollTerminal(t, srv.URL+"/v1/notifications/"+n.ID)
		if status != string(domain.StatusDelivered) {
			t.Errorf("notification %s: expected delivered, got %s", n.ID, status)
		}
	}

	// List should return all three.
	lresp := get(t, srv.URL+"/v1/notifications?channel=email")
	var list struct {
		Total int `json:"total"`
	}
	decode(t, lresp, &list)
	if list.Total != 3 {
		t.Errorf("expected total 3, got %d", list.Total)
	}
}

// TestEndToEnd_ValidationRejection verifies that a policy failure (opted-out
// recipient) ends as "failed", surfaced through the API.
func TestEndToEnd_ValidationRejection(t *testing.T) {
	application := app.Build(deterministicConfig(), discardLogger())
	application.Preferences.OptOut("blocked@example.com", domain.ChannelEmail)

	application.Pool.Start(t.Context())
	defer application.Pool.Shutdown(context.Background())

	srv := httptest.NewServer(application.Handler)
	defer srv.Close()

	body := `{"channel":"email","recipients":["blocked@example.com"],"message":{"body":"hello"}}`
	resp := post(t, srv.URL+"/v1/notifications", body)
	var submit struct {
		Notifications []struct {
			ID string `json:"id"`
		} `json:"notifications"`
	}
	decode(t, resp, &submit)

	status := pollTerminal(t, srv.URL+"/v1/notifications/"+submit.Notifications[0].ID)
	if status != string(domain.StatusFailed) {
		t.Errorf("expected failed for opted-out recipient, got %s", status)
	}
}

// --- helpers -----------------------------------------------------------------

// discardLogger silences logs during tests.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func pollTerminal(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp := get(t, url)
		var dto struct {
			Status string `json:"status"`
		}
		decode(t, resp, &dto)
		if dto.Status == string(domain.StatusDelivered) || dto.Status == string(domain.StatusFailed) {
			return dto.Status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("notification did not reach a terminal state in time: %s", url)
	return ""
}

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, string(b))
	}
}
