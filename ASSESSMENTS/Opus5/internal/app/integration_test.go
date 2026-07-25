package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/app"
	"github.com/evercifuentes/notifier/internal/config"
	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/platform/log"
	"github.com/evercifuentes/notifier/internal/preferences"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/provider/simulated"
	"github.com/evercifuentes/notifier/internal/ratelimit"
)

// These tests drive the fully wired application through its HTTP surface, so
// they cover the interaction between admission, validation, dispatch and
// persistence that unit tests deliberately isolate.

type notificationBody struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	Status  string `json:"status"`
	Summary struct {
		Total     int `json:"total"`
		Pending   int `json:"pending"`
		Sending   int `json:"sending"`
		Delivered int `json:"delivered"`
		Rejected  int `json:"rejected"`
		Failed    int `json:"failed"`
	} `json:"summary"`
	Deliveries []struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		Attempts      int    `json:"attempts"`
		Provider      string `json:"provider"`
		FailureCode   string `json:"failure_code"`
		FailureReason string `json:"failure_reason"`
		Recipient     struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"recipient"`
	} `json:"deliveries"`
}

// testConfig returns a configuration tuned for fast, deterministic tests.
func testConfig() config.Config {
	cfg := config.Default()
	cfg.Worker.Count = 8
	cfg.Worker.QueueCapacity = 256
	cfg.Limits.MaxRecipients = 100
	cfg.Provider.MinLatency = 0
	cfg.Provider.MaxLatency = 0
	cfg.Provider.TransientFailureRate = 0
	cfg.Provider.PermanentFailureRate = 0
	cfg.Retry.InitialBackoff = time.Millisecond
	cfg.Retry.MaxBackoff = 2 * time.Millisecond
	cfg.Server.ShutdownTimeout = 5 * time.Second
	return cfg
}

// newApp builds and starts a wired application, stopping it on cleanup.
func newApp(t *testing.T, cfg config.Config, deps app.Dependencies) *app.App {
	t.Helper()

	a := app.New(cfg, log.Discard(), deps)
	a.StartWorkers(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.StopWorkers(ctx); err != nil {
			t.Errorf("StopWorkers() = %v", err)
		}
	})
	return a
}

func post(t *testing.T, a *app.App, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, a *app.App, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) notificationBody {
	t.Helper()

	var body notificationBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return body
}

// awaitTerminal polls the API until the notification reaches a final state,
// which is exactly how a real client observes an asynchronous submission.
func awaitTerminal(t *testing.T, a *app.App, id string) notificationBody {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := get(t, a, "/v1/notifications/"+id)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want 200 (body: %s)", rec.Code, rec.Body)
		}
		body := decodeBody(t, rec)
		switch domain.Status(body.Status) {
		case domain.StatusDelivered, domain.StatusFailed, domain.StatusPartiallyDelivered:
			return body
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("notification %s never reached a terminal state", id)
	return notificationBody{}
}

func TestNotificationIsDeliveredEndToEnd(t *testing.T) {
	t.Parallel()

	a := newApp(t, testConfig(), app.Dependencies{})

	rec := post(t, a, `{
		"channel": "email",
		"recipients": [{"id":"u1","address":"a@example.com"},{"id":"u2","address":"b@example.com"}],
		"message": {"subject":"Welcome","body":"Hello there"},
		"metadata": {"campaign":"onboarding"}
	}`, nil)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202 (body: %s)", rec.Code, rec.Body)
	}
	created := decodeBody(t, rec)

	final := awaitTerminal(t, a, created.ID)
	if final.Status != string(domain.StatusDelivered) {
		t.Fatalf("status = %q, want %q (deliveries: %+v)", final.Status, domain.StatusDelivered, final.Deliveries)
	}
	if final.Summary.Delivered != 2 {
		t.Errorf("summary = %+v, want both deliveries delivered", final.Summary)
	}
	for _, d := range final.Deliveries {
		if d.Attempts != 1 {
			t.Errorf("delivery %s took %d attempts, want 1 against a healthy provider", d.ID, d.Attempts)
		}
		if d.Provider != "simulated-email" {
			t.Errorf("delivery %s recorded provider %q, want the email provider", d.ID, d.Provider)
		}
	}
}

// Each recipient is validated and delivered independently: one opted-out user
// must not stop the rest of the batch.
func TestOneRejectedRecipientDoesNotBlockTheOthers(t *testing.T) {
	t.Parallel()

	prefs := preferences.NewMemoryStore(map[string]preferences.Preferences{
		"opted-out": {OptedOut: map[domain.Channel]bool{domain.ChannelEmail: true}},
		"blocked":   {Blocked: true},
	})
	a := newApp(t, testConfig(), app.Dependencies{Preferences: prefs})

	rec := post(t, a, `{
		"channel": "email",
		"recipients": [
			{"id":"ok","address":"ok@example.com"},
			{"id":"opted-out","address":"out@example.com"},
			{"id":"blocked","address":"blocked@example.com"}
		],
		"message": {"subject":"Hi","body":"Hello"}
	}`, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202 (body: %s)", rec.Code, rec.Body)
	}

	final := awaitTerminal(t, a, decodeBody(t, rec).ID)
	if final.Status != string(domain.StatusPartiallyDelivered) {
		t.Errorf("status = %q, want %q", final.Status, domain.StatusPartiallyDelivered)
	}
	if final.Summary.Delivered != 1 || final.Summary.Rejected != 2 {
		t.Errorf("summary = %+v, want 1 delivered and 2 rejected", final.Summary)
	}

	byRecipient := make(map[string]string, len(final.Deliveries))
	reasons := make(map[string]string, len(final.Deliveries))
	for _, d := range final.Deliveries {
		byRecipient[d.Recipient.ID] = d.Status
		reasons[d.Recipient.ID] = d.FailureCode
	}
	if byRecipient["ok"] != string(domain.DeliveryDelivered) {
		t.Errorf("the acceptable recipient ended as %q", byRecipient["ok"])
	}
	// The rejection reason must be specific enough to act on.
	if reasons["opted-out"] != "recipient_opted_out" {
		t.Errorf("opted-out recipient failure code = %q", reasons["opted-out"])
	}
	if reasons["blocked"] != "recipient_blocked" {
		t.Errorf("blocked recipient failure code = %q", reasons["blocked"])
	}
}

// A provider that always fails must exhaust the retry budget and be recorded
// as failed, not retried forever and not silently dropped.
func TestAnUnavailableProviderIsRetriedThenFailed(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Provider.TransientFailureRate = 1.0 // every call fails transiently
	cfg.Retry.MaxAttempts = 3
	cfg.Breaker.FailureThreshold = 0 // breaker disabled, so retries are observable

	a := newApp(t, cfg, app.Dependencies{})

	rec := post(t, a, `{
		"channel": "sms",
		"recipients": [{"id":"u1","address":"+14155550123"}],
		"message": {"body":"Hello"}
	}`, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202 (body: %s)", rec.Code, rec.Body)
	}

	final := awaitTerminal(t, a, decodeBody(t, rec).ID)
	if final.Status != string(domain.StatusFailed) {
		t.Fatalf("status = %q, want %q", final.Status, domain.StatusFailed)
	}
	d := final.Deliveries[0]
	if d.Attempts != 3 {
		t.Errorf("Attempts = %d, want the full retry budget of 3", d.Attempts)
	}
	if d.FailureCode == "" || d.FailureReason == "" {
		t.Error("a failed delivery must record why it failed")
	}
}

// The breaker must stop calling a provider that is clearly down, rather than
// letting every delivery pay the full retry cost.
func TestTheCircuitBreakerShedsLoadFromADeadProvider(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Provider.TransientFailureRate = 1.0
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 3
	cfg.Breaker.OpenDuration = time.Hour

	a := newApp(t, cfg, app.Dependencies{})

	recipients := make([]string, 0, 20)
	for i := range 20 {
		recipients = append(recipients, fmt.Sprintf(`{"id":"u%d","address":"user%d@example.com"}`, i, i))
	}
	body := fmt.Sprintf(`{"channel":"email","recipients":[%s],"message":{"subject":"Hi","body":"Hello"}}`,
		strings.Join(recipients, ","))

	rec := post(t, a, body, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202 (body: %s)", rec.Code, rec.Body)
	}

	final := awaitTerminal(t, a, decodeBody(t, rec).ID)
	if final.Summary.Failed != 20 {
		t.Fatalf("summary = %+v, want every delivery to fail against a dead provider", final.Summary)
	}

	// Once open, later deliveries fail without a provider call at all.
	skipped := 0
	for _, d := range final.Deliveries {
		if d.Attempts == 0 {
			skipped++
		}
	}
	if skipped == 0 {
		t.Error("no delivery was shed by the breaker; every one still called the failing provider")
	}
}

// Rate limits are enforced per recipient and channel, and a throttled
// delivery is rejected rather than silently dropped.
func TestRateLimitingRejectsExcessDeliveries(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	limiter := ratelimit.New(ratelimit.Config{Rate: 1, Interval: time.Hour, Burst: 1})
	a := newApp(t, cfg, app.Dependencies{Limiter: limiter})

	body := `{"channel":"email","recipients":[{"id":"u1","address":"u1@example.com"}],"message":{"subject":"Hi","body":"Hello"}}`

	first := awaitTerminal(t, a, decodeBody(t, post(t, a, body, nil)).ID)
	if first.Status != string(domain.StatusDelivered) {
		t.Fatalf("first notification status = %q, want %q", first.Status, domain.StatusDelivered)
	}

	second := awaitTerminal(t, a, decodeBody(t, post(t, a, body, nil)).ID)
	if second.Status != string(domain.StatusFailed) {
		t.Fatalf("second notification status = %q, want it rejected by the rate limit", second.Status)
	}
	if got := second.Deliveries[0].FailureCode; got != "rate_limited" {
		t.Errorf("failure code = %q, want %q", got, "rate_limited")
	}
	if got := second.Deliveries[0].Attempts; got != 0 {
		t.Errorf("Attempts = %d, want 0: a throttled delivery must never reach the provider", got)
	}
}

// A burst larger than the queue must be shed cleanly rather than degrade the
// whole service.
func TestBurstBeyondCapacityIsShedWithBackpressure(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Worker.QueueCapacity = 4
	cfg.Limits.MaxRecipients = 4
	// Workers block on a slow provider so the queue actually fills up.
	cfg.Provider.MinLatency = 200 * time.Millisecond
	cfg.Provider.MaxLatency = 200 * time.Millisecond
	cfg.Worker.Count = 1

	a := newApp(t, cfg, app.Dependencies{})

	body := `{"channel":"email","recipients":[
		{"address":"a@example.com"},{"address":"b@example.com"},
		{"address":"c@example.com"},{"address":"d@example.com"}
	],"message":{"subject":"Hi","body":"Hello"}}`

	var accepted, shed int
	for range 8 {
		switch code := post(t, a, body, nil).Code; code {
		case http.StatusAccepted:
			accepted++
		case http.StatusTooManyRequests:
			shed++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}

	if accepted == 0 {
		t.Error("no request was accepted; the service shed everything")
	}
	if shed == 0 {
		t.Error("no request was shed; the queue bound was not enforced")
	}
	// Shedding must not break the service: it still answers correctly.
	if rec := get(t, a, "/v1/channels"); rec.Code != http.StatusOK {
		t.Errorf("the service stopped answering during backpressure: status %d", rec.Code)
	}
}

func TestListReflectsSubmittedNotifications(t *testing.T) {
	t.Parallel()

	a := newApp(t, testConfig(), app.Dependencies{})

	for i := range 3 {
		body := fmt.Sprintf(
			`{"channel":"sms","recipients":[{"address":"+1415555012%d"}],"message":{"body":"Hello"}}`, i)
		if rec := post(t, a, body, nil); rec.Code != http.StatusAccepted {
			t.Fatalf("submit %d status = %d: %s", i, rec.Code, rec.Body)
		}
	}

	rec := get(t, a, "/v1/notifications?channel=sms&limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}

	var list struct {
		Items      []notificationBody `json:"items"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding the list response: %v", err)
	}
	if list.Pagination.Total != 3 {
		t.Errorf("Total = %d, want 3", list.Pagination.Total)
	}
}

// A channel is added by registering a provider; nothing else should need to
// change for it to become submittable and queryable.
func TestANewChannelNeedsOnlyAProvider(t *testing.T) {
	t.Parallel()

	const webhook = domain.Channel("webhook")

	registry := provider.NewRegistry()
	registry.MustRegister(
		simulated.NewEmail(simulated.Behaviour{}),
		// The only new code a channel requires: a provider with its own
		// address rules.
		simulated.New("simulated-webhook", webhook, simulated.Behaviour{}, func(addr string) error {
			if len(addr) < 8 || addr[:8] != "https://" {
				return fmt.Errorf("must be an https URL")
			}
			return nil
		}),
	)

	a := newApp(t, testConfig(), app.Dependencies{Registry: registry})

	if rec := get(t, a, "/v1/channels"); !bytes.Contains(rec.Body.Bytes(), []byte("webhook")) {
		t.Errorf("the new channel is not advertised: %s", rec.Body)
	}

	rec := post(t, a, `{
		"channel":"webhook",
		"recipients":[{"id":"svc","address":"https://hooks.example.com/abc"}],
		"message":{"subject":"Hi","body":"Hello"}
	}`, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit on the new channel = %d, want 202 (body: %s)", rec.Code, rec.Body)
	}

	final := awaitTerminal(t, a, decodeBody(t, rec).ID)
	if final.Status != string(domain.StatusDelivered) {
		t.Errorf("status = %q, want %q", final.Status, domain.StatusDelivered)
	}

	// The channel's own address rule is enforced on the request path.
	bad := post(t, a, `{
		"channel":"webhook",
		"recipients":[{"address":"ftp://example.com"}],
		"message":{"subject":"Hi","body":"Hello"}
	}`, nil)
	if bad.Code != http.StatusBadRequest {
		t.Errorf("an address violating the new channel's rule returned %d, want 400", bad.Code)
	}
}

// Work already accepted from clients must not be discarded on shutdown.
func TestShutdownDrainsAcceptedWork(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Worker.Count = 2
	cfg.Provider.MinLatency = 5 * time.Millisecond
	cfg.Provider.MaxLatency = 5 * time.Millisecond

	a := app.New(cfg, log.Discard(), app.Dependencies{})
	a.StartWorkers(context.Background())

	recipients := make([]string, 0, 40)
	for i := range 40 {
		recipients = append(recipients, fmt.Sprintf(`{"address":"user%d@example.com"}`, i))
	}
	rec := post(t, a, fmt.Sprintf(
		`{"channel":"email","recipients":[%s],"message":{"subject":"Hi","body":"Hello"}}`,
		strings.Join(recipients, ",")), nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d: %s", rec.Code, rec.Body)
	}
	id := decodeBody(t, rec).ID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.StopWorkers(ctx); err != nil {
		t.Fatalf("StopWorkers() = %v, want a clean drain", err)
	}

	final := decodeBody(t, get(t, a, "/v1/notifications/"+id))
	if final.Summary.Pending != 0 || final.Summary.Sending != 0 {
		t.Errorf("summary = %+v, want no work left in flight after a clean drain", final.Summary)
	}
	if final.Summary.Delivered != 40 {
		t.Errorf("delivered %d of 40 after draining", final.Summary.Delivered)
	}
	if a.QueueDepth() != 0 {
		t.Errorf("queue depth = %d after shutdown, want 0", a.QueueDepth())
	}
}

// Once shut down, the service must refuse work rather than accept it into a
// queue nobody is draining.
func TestSubmissionsAfterShutdownAreRefused(t *testing.T) {
	t.Parallel()

	a := app.New(testConfig(), log.Discard(), app.Dependencies{})
	a.StartWorkers(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.StopWorkers(ctx); err != nil {
		t.Fatalf("StopWorkers() = %v", err)
	}

	rec := post(t, a, `{"channel":"email","recipients":[{"address":"a@example.com"}],"message":{"subject":"Hi","body":"Hello"}}`, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("submit after shutdown = %d, want 429", rec.Code)
	}
	if rec := get(t, a, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz after shutdown = %d, want 503", rec.Code)
	}
}
