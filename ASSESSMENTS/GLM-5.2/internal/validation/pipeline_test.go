package validation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/notification-service/internal/domain"
)

func validEmailNotification() *domain.Notification {
	return &domain.Notification{
		Channel: domain.ChannelEmail,
		Message: domain.Message{Subject: "Hi", Body: "Hello"},
	}
}

func TestRecipientValidator(t *testing.T) {
	v := NewRecipientValidator()
	ctx := context.Background()

	if err := v.Validate(ctx, Input{Notification: validEmailNotification(), Recipient: domain.Recipient{Email: "a@b.com"}}); err != nil {
		t.Fatalf("valid email should pass: %v", err)
	}
	if err := v.Validate(ctx, Input{Notification: validEmailNotification(), Recipient: domain.Recipient{Email: ""}}); err == nil {
		t.Fatal("missing email should fail")
	}
	if err := v.Validate(ctx, Input{Notification: validEmailNotification(), Recipient: domain.Recipient{Email: "not-an-email"}}); err == nil {
		t.Fatal("invalid email should fail")
	}

	smsN := &domain.Notification{Channel: domain.ChannelSMS, Message: domain.Message{Body: "hi"}}
	if err := v.Validate(ctx, Input{Notification: smsN, Recipient: domain.Recipient{Phone: "+14155550123"}}); err != nil {
		t.Fatalf("valid phone should pass: %v", err)
	}
	if err := v.Validate(ctx, Input{Notification: smsN, Recipient: domain.Recipient{Phone: "123"}}); err == nil {
		t.Fatal("invalid phone should fail")
	}

	pushN := &domain.Notification{Channel: domain.ChannelPush, Message: domain.Message{Body: "hi"}}
	if err := v.Validate(ctx, Input{Notification: pushN, Recipient: domain.Recipient{PushToken: "token123"}}); err != nil {
		t.Fatalf("valid token should pass: %v", err)
	}
	if err := v.Validate(ctx, Input{Notification: pushN, Recipient: domain.Recipient{PushToken: ""}}); err == nil {
		t.Fatal("missing token should fail")
	}
}

func TestContentValidator(t *testing.T) {
	v := NewContentValidator()
	ctx := context.Background()

	if err := v.Validate(ctx, Input{Notification: validEmailNotification(), Recipient: domain.Recipient{ID: "x"}}); err != nil {
		t.Fatalf("valid email content should pass: %v", err)
	}
	noSubject := &domain.Notification{Channel: domain.ChannelEmail, Message: domain.Message{Body: "x"}}
	if err := v.Validate(ctx, Input{Notification: noSubject, Recipient: domain.Recipient{ID: "x"}}); err == nil {
		t.Fatal("email without subject should fail")
	}
	emptyBody := &domain.Notification{Channel: domain.ChannelSMS, Message: domain.Message{}}
	if err := v.Validate(ctx, Input{Notification: emptyBody, Recipient: domain.Recipient{ID: "x"}}); err == nil {
		t.Fatal("sms without body should fail")
	}
	pushWithData := &domain.Notification{Channel: domain.ChannelPush, Message: domain.Message{Data: map[string]string{"k": "v"}}}
	if err := v.Validate(ctx, Input{Notification: pushWithData, Recipient: domain.Recipient{ID: "x"}}); err != nil {
		t.Fatalf("push with data only should pass: %v", err)
	}
}

type stubPrefStore struct{ allow bool }

func (s stubPrefStore) Allows(context.Context, string, domain.Channel) (bool, error) {
	return s.allow, nil
}

func TestPreferenceValidator(t *testing.T) {
	v := NewPreferenceValidator(stubPrefStore{allow: false})
	err := v.Validate(context.Background(), Input{Notification: validEmailNotification(), Recipient: domain.Recipient{ID: "u1"}})
	if err == nil {
		t.Fatal("opted-out recipient should fail")
	}
	var ve *domain.ValidationErr
	if !errors.As(err, &ve) || ve.Code != "channel_opted_out" {
		t.Fatalf("expected channel_opted_out, got %v", err)
	}
}

func TestSlidingWindowLimiter(t *testing.T) {
	l := NewSlidingWindowLimiter(100*time.Millisecond, 2)
	ctx := context.Background()
	mustAllow := func() {
		t.Helper()
		ok, err := l.Allow(ctx, "u1", domain.ChannelEmail)
		if err != nil || !ok {
			t.Fatalf("expected allow, got %v %v", ok, err)
		}
	}
	mustAllow()
	mustAllow()
	ok, _ := l.Allow(ctx, "u1", domain.ChannelEmail)
	if ok {
		t.Fatal("third request within window should be denied")
	}
	// Different key is independent.
	ok, _ = l.Allow(ctx, "u2", domain.ChannelEmail)
	if !ok {
		t.Fatal("different recipient should be allowed")
	}
}

// slowValidator sleeps for a while, but aborts early when ctx is cancelled.
type slowValidator struct {
	started   atomic.Bool
	cancelled atomic.Bool
	delay     time.Duration
}

func (s *slowValidator) Name() string { return "slow" }
func (s *slowValidator) Validate(ctx context.Context, _ Input) error {
	s.started.Store(true)
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		s.cancelled.Store(true)
		return ctx.Err()
	}
}

// fastFailValidator fails immediately.
type fastFailValidator struct{}

func (fastFailValidator) Name() string { return "fast_fail" }
func (fastFailValidator) Validate(ctx context.Context, _ Input) error {
	return domain.NewValidationError("always_fail", "", "boom")
}

// TestPipelineFailFast verifies that when one validator fails fast, the other
// (slow) validator is cancelled and does not consume resources for its full
// duration.
func TestPipelineFailFast(t *testing.T) {
	slow := &slowValidator{delay: 2 * time.Second}
	p := NewPipeline(slow, fastFailValidator{})

	start := time.Now()
	errs := p.Run(context.Background(), Input{Notification: validEmailNotification(), Recipient: domain.Recipient{ID: "x"}})
	elapsed := time.Since(start)

	if len(errs) == 0 {
		t.Fatal("expected at least one validation error")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("pipeline did not fail-fast: took %v", elapsed)
	}
	if !slow.cancelled.Load() {
		t.Fatal("slow validator should have been cancelled on fail-fast")
	}
}

func TestPipelineAllPass(t *testing.T) {
	p := NewPipeline(NewRecipientValidator(), NewContentValidator())
	errs := p.Run(context.Background(), Input{
		Notification: validEmailNotification(),
		Recipient:    domain.Recipient{ID: "x", Email: "a@b.com"},
	})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestPipelineRespectsContextCancellation(t *testing.T) {
	slow := &slowValidator{delay: 5 * time.Second}
	p := NewPipeline(slow)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_ = p.Run(ctx, Input{Notification: validEmailNotification(), Recipient: domain.Recipient{ID: "x"}})
	if time.Since(start) >= 5*time.Second {
		t.Fatal("pipeline should have aborted on ctx cancellation")
	}
	if !slow.cancelled.Load() {
		t.Fatal("slow validator should have been cancelled")
	}
}
