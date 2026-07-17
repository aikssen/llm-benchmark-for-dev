package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/aikssen/notification-service/internal/domain"
)

type stub struct{ ch domain.Channel }

func (s stub) Channel() domain.Channel                          { return s.ch }
func (s stub) Send(context.Context, *domain.Notification) error { return nil }

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(stub{ch: domain.ChannelEmail})

	p, err := r.Get(domain.ChannelEmail)
	if err != nil {
		t.Fatalf("expected provider, got %v", err)
	}
	if p.Channel() != domain.ChannelEmail {
		t.Errorf("wrong provider channel: %s", p.Channel())
	}

	if _, err := r.Get(domain.ChannelSMS); !errors.Is(err, domain.ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestRegistry_Channels(t *testing.T) {
	r := NewRegistry()
	r.Register(stub{ch: domain.ChannelEmail})
	r.Register(stub{ch: domain.ChannelPush})
	if got := len(r.Channels()); got != 2 {
		t.Errorf("expected 2 channels, got %d", got)
	}
}

func TestRegistry_RegisterNilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering nil should panic")
		}
	}()
	NewRegistry().Register(nil)
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("nil error is not retryable")
	}
	if !IsRetryable(Retryable(errors.New("x"))) {
		t.Error("explicitly retryable should be retryable")
	}
	if IsRetryable(Permanent(errors.New("x"))) {
		t.Error("permanent should not be retryable")
	}
	// Unclassified errors default to retryable.
	if !IsRetryable(errors.New("plain")) {
		t.Error("unclassified error should default to retryable")
	}
}
