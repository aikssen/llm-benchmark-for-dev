package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
)

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	p := NewSimulatedProvider(domain.ChannelEmail, 0, 0, 0)
	r.Register(p)

	got, err := r.Get(domain.ChannelEmail)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel() != domain.ChannelEmail {
		t.Fatalf("channel=%s", got.Channel())
	}

	_, err = r.Get(domain.ChannelSMS)
	if err == nil {
		t.Fatal("expected missing provider error")
	}
}

func TestSimulatedProviderSuccess(t *testing.T) {
	p := NewSimulatedProvider(domain.ChannelSMS, 0, time.Millisecond, time.Millisecond)
	err := p.Send(context.Background(), &domain.Notification{Recipient: "+15551234567"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSimulatedProviderAlwaysFail(t *testing.T) {
	p := NewSimulatedProvider(domain.ChannelPush, 1, 0, 0)
	err := p.Send(context.Background(), &domain.Notification{Recipient: "token-12345"})
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("expected temporary error, got %v", err)
	}
}

func TestSimulatedProviderContextCancel(t *testing.T) {
	p := NewSimulatedProvider(domain.ChannelEmail, 0, 200*time.Millisecond, 200*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := p.Send(ctx, &domain.Notification{Recipient: "a@b.com"})
	if err == nil {
		t.Fatal("expected context error")
	}
}
