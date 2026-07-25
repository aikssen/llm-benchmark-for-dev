package simulated_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/provider/simulated"
)

// fixedRandom returns a scripted sequence, so the simulated behaviour is
// deterministic in tests.
func fixedRandom(values ...float64) func() float64 {
	var (
		mu sync.Mutex
		i  int
	)
	return func() float64 {
		mu.Lock()
		defer mu.Unlock()
		v := values[i%len(values)]
		i++
		return v
	}
}

// instantSleep removes the artificial latency from tests.
func instantSleep() simulated.Option {
	return simulated.WithSleep(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
}

func request() provider.SendRequest {
	return provider.SendRequest{
		NotificationID: "ntf_1",
		DeliveryID:     "dlv_1",
		Recipient:      domain.Recipient{Address: "user@example.com"},
		Message:        domain.Message{Subject: "Hi", Body: "Hello"},
		Attempt:        1,
	}
}

func TestSendSucceedsWhenTheRollIsAboveTheFailureRates(t *testing.T) {
	t.Parallel()

	p := simulated.NewEmail(
		simulated.Behaviour{PermanentFailureRate: 0.1, TransientFailureRate: 0.2},
		simulated.WithRandom(fixedRandom(0.9)),
		instantSleep(),
	)

	res, err := p.Send(context.Background(), request())
	if err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}
	if res.MessageID == "" {
		t.Error("a successful send must return a provider message id")
	}
}

func TestSendClassifiesFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		roll      float64
		wantCode  string
		wantRetry bool
	}{
		// Permanent occupies [0, 0.1), transient [0.1, 0.3).
		{"permanent", 0.05, provider.CodeRejected, false},
		{"transient", 0.2, provider.CodeUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := simulated.NewEmail(
				simulated.Behaviour{PermanentFailureRate: 0.1, TransientFailureRate: 0.2},
				simulated.WithRandom(fixedRandom(tt.roll)),
				instantSleep(),
			)

			_, err := p.Send(context.Background(), request())
			var perr *provider.Error
			if !errors.As(err, &perr) {
				t.Fatalf("Send() = %v, want a *provider.Error", err)
			}
			if perr.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", perr.Code, tt.wantCode)
			}
			if perr.Retryable() != tt.wantRetry {
				t.Errorf("Retryable() = %v, want %v", perr.Retryable(), tt.wantRetry)
			}
		})
	}
}

// A call cut short by the caller's deadline is retryable: the message may or
// may not have been accepted, and giving up permanently would lose it.
func TestSendReportsATimeoutAsRetryable(t *testing.T) {
	t.Parallel()

	p := simulated.NewEmail(
		simulated.Behaviour{MinLatency: time.Hour, MaxLatency: time.Hour},
		simulated.WithRandom(fixedRandom(0.99)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := p.Send(ctx, request())
	var perr *provider.Error
	if !errors.As(err, &perr) {
		t.Fatalf("Send() = %v, want a *provider.Error", err)
	}
	if perr.Code != provider.CodeTimeout {
		t.Errorf("code = %q, want %q", perr.Code, provider.CodeTimeout)
	}
	if !perr.Retryable() {
		t.Error("a timeout must be retryable")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("the underlying context error should remain inspectable")
	}
}

func TestMessageIDsAreUnique(t *testing.T) {
	t.Parallel()

	p := simulated.NewSMS(simulated.Behaviour{}, simulated.WithRandom(fixedRandom(0.99)), instantSleep())

	const n = 200
	var (
		mu  sync.Mutex
		ids = make(map[string]struct{}, n)
		wg  sync.WaitGroup
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := p.Send(context.Background(), request())
			if err != nil {
				t.Errorf("Send() = %v", err)
				return
			}
			mu.Lock()
			ids[res.MessageID] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(ids) != n {
		t.Errorf("got %d distinct message ids from %d sends, want them all unique", len(ids), n)
	}
}

func TestProvidersDeclareTheirChannel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		p    provider.Provider
		want domain.Channel
	}{
		{simulated.NewEmail(simulated.Behaviour{}), domain.ChannelEmail},
		{simulated.NewSMS(simulated.Behaviour{}), domain.ChannelSMS},
		{simulated.NewPush(simulated.Behaviour{}), domain.ChannelPush},
	}
	for _, tt := range tests {
		if got := tt.p.Channel(); got != tt.want {
			t.Errorf("%s: Channel() = %q, want %q", tt.p.Name(), got, tt.want)
		}
	}
}

func TestAddressValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fn      func(string) error
		address string
		wantErr bool
	}{
		{"plain email", simulated.ValidateEmail, "user@example.com", false},
		{"email with plus tag", simulated.ValidateEmail, "user+tag@example.co.uk", false},
		{"email with display name", simulated.ValidateEmail, "User <user@example.com>", true},
		{"email with spaces", simulated.ValidateEmail, " user@example.com ", true},
		{"empty email", simulated.ValidateEmail, "", true},
		{"e164 phone", simulated.ValidatePhone, "+14155550123", false},
		{"phone without plus", simulated.ValidatePhone, "14155550123", true},
		{"phone starting with zero", simulated.ValidatePhone, "+04155550123", true},
		{"phone too short", simulated.ValidatePhone, "+1234", true},
		{"push token", simulated.ValidatePushToken, "dGhpcy1pcy1hLXRva2Vu:APA91b", false},
		{"push token too short", simulated.ValidatePushToken, "abc", true},
		{"push token with spaces", simulated.ValidatePushToken, "abcdefghijklmnop qrst", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.fn(tt.address)
			if (err != nil) != tt.wantErr {
				t.Errorf("validating %q returned %v, wantErr = %v", tt.address, err, tt.wantErr)
			}
		})
	}
}

func TestLatencyStaysWithinTheConfiguredWindow(t *testing.T) {
	t.Parallel()

	var slept []time.Duration
	p := simulated.NewPush(
		simulated.Behaviour{MinLatency: 10 * time.Millisecond, MaxLatency: 50 * time.Millisecond},
		simulated.WithRandom(fixedRandom(0.0, 0.5, 0.99)),
		simulated.WithSleep(func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		}),
	)

	for range 3 {
		_, _ = p.Send(context.Background(), request())
	}
	for _, d := range slept {
		if d < 10*time.Millisecond || d > 50*time.Millisecond {
			t.Errorf("latency %s is outside the configured [10ms, 50ms] window", d)
		}
	}
}
