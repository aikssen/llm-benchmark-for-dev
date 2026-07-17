package api_test

// End-to-end test: real HTTP API + real worker pool + real validation
// pipeline + simulated providers, exercising the full submit -> process ->
// query flow.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"notificationservice/internal/api"
	"notificationservice/internal/domain"
	"notificationservice/internal/provider"
	"notificationservice/internal/store"
	"notificationservice/internal/validation"
	"notificationservice/internal/worker"
)

func startService(t *testing.T) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	st := store.NewMemory()

	registry := provider.NewRegistry()
	simCfg := provider.SimulatedConfig{
		MinLatency:           time.Millisecond,
		MaxLatency:           5 * time.Millisecond,
		TransientFailureRate: 0, // deterministic outcome for the test
		PermanentFailureRate: 0,
	}
	for _, ch := range domain.Channels() {
		if err := registry.Register(provider.NewSimulated(ch, simCfg, logger)); err != nil {
			t.Fatal(err)
		}
	}

	pipeline := validation.NewPipeline(
		validation.Recipient{},
		validation.Content{},
		validation.Preferences{Checker: validation.NewStaticPreferences(time.Millisecond, "optout@example.com")},
		validation.NewRateLimit(100, time.Minute),
	)

	pool := worker.New(worker.Config{
		Workers:           4,
		QueueCapacity:     64,
		MaxAttempts:       3,
		RetryBaseDelay:    time.Millisecond,
		RetryMaxDelay:     10 * time.Millisecond,
		ValidationTimeout: time.Second,
		SendTimeout:       time.Second,
	}, st, registry, pipeline, logger)

	ctx := t.Context()
	pool.Start(ctx)
	t.Cleanup(pool.Wait)

	handler := api.New(api.Config{
		MaxRecipients:   10,
		MaxBodyBytes:    1 << 20,
		DefaultPageSize: 20,
		MaxPageSize:     100,
	}, st, pool, logger)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

type notificationJSON struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Deliveries []struct {
		Recipient string `json:"recipient"`
		Status    string `json:"status"`
		Error     string `json:"error"`
	} `json:"deliveries"`
}

func TestEndToEndDelivery(t *testing.T) {
	srv := startService(t)

	// Submit one email to a valid recipient and one to an opted-out one.
	resp, err := http.Post(srv.URL+"/v1/notifications", "application/json", strings.NewReader(`{
		"channel": "email",
		"recipients": ["ok@example.com", "optout@example.com"],
		"message": {"subject": "Greetings", "body": "Hello there"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202", resp.StatusCode)
	}
	var created notificationJSON
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Poll until the notification reaches a terminal aggregate state.
	var final notificationJSON
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("notification never reached a terminal state, last: %+v", final)
		case <-time.After(10 * time.Millisecond):
		}
		r, err := http.Get(srv.URL + "/v1/notifications/" + created.ID)
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(r.Body).Decode(&final)
		r.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if final.Status == string(domain.StatusPartiallyDelivered) ||
			final.Status == string(domain.StatusDelivered) ||
			final.Status == string(domain.StatusFailed) {
			break
		}
	}

	// The valid recipient is delivered; the opted-out one fails validation.
	if final.Status != string(domain.StatusPartiallyDelivered) {
		t.Fatalf("final status = %q, want partially_delivered (%+v)", final.Status, final)
	}
	for _, d := range final.Deliveries {
		switch d.Recipient {
		case "ok@example.com":
			if d.Status != string(domain.DeliveryDelivered) {
				t.Errorf("ok recipient status = %q (%s), want delivered", d.Status, d.Error)
			}
		case "optout@example.com":
			if d.Status != string(domain.DeliveryFailed) || !strings.Contains(d.Error, "opted out") {
				t.Errorf("optout recipient = %+v, want failed with opt-out error", d)
			}
		default:
			t.Errorf("unexpected recipient %q", d.Recipient)
		}
	}

	// The notification shows up in the listing with its terminal status.
	lr, err := http.Get(srv.URL + "/v1/notifications?status=partially_delivered")
	if err != nil {
		t.Fatal(err)
	}
	defer lr.Body.Close()
	var list struct {
		Items []notificationJSON `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.NewDecoder(lr.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].ID != created.ID {
		t.Errorf("listing = %+v, want the submitted notification", list)
	}
}

func TestEndToEndBurstWithBackpressure(t *testing.T) {
	srv := startService(t)

	// Fire a burst of requests; every request must get a definitive answer:
	// either accepted (202) or cleanly rejected for capacity (429).
	const burst = 50
	results := make(chan int, burst)
	for i := range burst {
		go func() {
			body := fmt.Sprintf(`{
				"channel": "sms",
				"recipients": ["+1415555%04d"],
				"message": {"body": "burst"}
			}`, i)
			resp, err := http.Post(srv.URL+"/v1/notifications", "application/json", strings.NewReader(body))
			if err != nil {
				results <- 0
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}()
	}

	accepted, rejected := 0, 0
	for range burst {
		switch <-results {
		case http.StatusAccepted:
			accepted++
		case http.StatusTooManyRequests:
			rejected++
		default:
			t.Error("burst request got an unexpected result")
		}
	}
	if accepted == 0 {
		t.Error("no requests were accepted during the burst")
	}
	t.Logf("burst: %d accepted, %d rejected with backpressure", accepted, rejected)
}
