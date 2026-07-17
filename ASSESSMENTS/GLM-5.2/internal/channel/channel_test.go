package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/provider"
)

// recordingProvider captures the outbound it receives.
type recordingProvider struct {
	last provider.Outbound
	err  error
}

func (r *recordingProvider) Name() string { return "recorder" }
func (r *recordingProvider) Send(_ context.Context, out provider.Outbound) error {
	r.last = out
	return r.err
}

func TestEmailChannel(t *testing.T) {
	rp := &recordingProvider{}
	ch := NewEmail(rp)
	if ch.Name() != domain.ChannelEmail {
		t.Fatalf("got %s", ch.Name())
	}
	n := &domain.Notification{Message: domain.Message{Subject: "S", Body: "B"}}
	if err := ch.Send(context.Background(), n, domain.Recipient{ID: "u", Email: "a@b.com"}); err != nil {
		t.Fatal(err)
	}
	if rp.last.To != "a@b.com" || rp.last.Subject != "S" || rp.last.Body != "B" {
		t.Fatalf("unexpected outbound: %+v", rp.last)
	}
}

func TestSMSChannel(t *testing.T) {
	rp := &recordingProvider{}
	ch := NewSMS(rp)
	n := &domain.Notification{Message: domain.Message{Body: "B"}}
	if err := ch.Send(context.Background(), n, domain.Recipient{ID: "u", Phone: "+14155550123"}); err != nil {
		t.Fatal(err)
	}
	if rp.last.To != "+14155550123" || rp.last.Body != "B" {
		t.Fatalf("unexpected outbound: %+v", rp.last)
	}
}

func TestPushChannel(t *testing.T) {
	rp := &recordingProvider{}
	ch := NewPush(rp)
	n := &domain.Notification{Message: domain.Message{Body: "B", Data: map[string]string{"k": "v"}}}
	if err := ch.Send(context.Background(), n, domain.Recipient{ID: "u", PushToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	if rp.last.To != "tok" || rp.last.Body != "B" || rp.last.Data["k"] != "v" {
		t.Fatalf("unexpected outbound: %+v", rp.last)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry().Register(NewEmail(&recordingProvider{}))
	if _, err := r.Get(domain.ChannelEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(domain.ChannelSMS); !errors.Is(err, domain.ErrChannelUnsupported) {
		t.Fatalf("expected ErrChannelUnsupported, got %v", err)
	}
	if len(r.Supported()) != 1 {
		t.Fatalf("expected 1 supported channel, got %d", len(r.Supported()))
	}
}
