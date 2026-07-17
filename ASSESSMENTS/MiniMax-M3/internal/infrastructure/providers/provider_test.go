package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
)

type stubProvider struct {
	name    string
	calls   int
	fail    bool
	delay   time.Duration
	channel domain.Channel
}

func (s *stubProvider) Name() string               { return s.name }
func (s *stubProvider) Channels() []domain.Channel { return []domain.Channel{s.channel} }
func (s *stubProvider) Send(_ context.Context, _ *domain.Notification) error {
	s.calls++
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.fail {
		return errors.New("nope")
	}
	return nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "p", channel: domain.ChannelEmail}
	r.Register(p)
	got, ok := r.Get(domain.ChannelEmail)
	if !ok || got != p {
		t.Fatalf("Get failed")
	}
	if _, ok := r.Get(domain.ChannelSMS); ok {
		t.Fatalf("expected missing for sms")
	}
}

func TestSimulatedProvider_RespectsContext(t *testing.T) {
	p := NewSimulated(SimulatedConfig{
		Name:       "p",
		Channels:   []domain.Channel{domain.ChannelEmail},
		LatencyMin: 100 * time.Millisecond,
		LatencyMax: 100 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	err := p.Send(ctx, domain.NewNotification("i", "a@b", domain.ChannelEmail, domain.Message{Body: "x"}, time.Now()))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected deadline exceeded, got %v", err)
	}
}
