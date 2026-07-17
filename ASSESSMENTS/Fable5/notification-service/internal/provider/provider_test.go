package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"notificationservice/internal/domain"
)

type stubProvider struct{ ch domain.Channel }

func (s stubProvider) Name() string            { return "stub-" + string(s.ch) }
func (s stubProvider) Channel() domain.Channel { return s.ch }
func (stubProvider) Send(context.Context, domain.Notification, string) error {
	return nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubProvider{ch: domain.ChannelEmail}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(stubProvider{ch: domain.ChannelEmail}); err == nil {
		t.Fatal("registering a second provider for the same channel should fail")
	}

	p, err := r.Get(domain.ChannelEmail)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Channel() != domain.ChannelEmail {
		t.Errorf("got provider for %q", p.Channel())
	}

	if _, err := r.Get(domain.ChannelSMS); !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestPermanentErrors(t *testing.T) {
	base := errors.New("hard bounce")
	perm := Permanent(base)

	if !IsPermanent(perm) {
		t.Error("Permanent error not detected")
	}
	if !errors.Is(perm, base) {
		t.Error("Permanent must preserve the error chain")
	}
	if IsPermanent(base) {
		t.Error("plain error must not be permanent")
	}
	if IsPermanent(nil) || Permanent(nil) != nil {
		t.Error("nil handling is wrong")
	}
	// Wrapping a permanent error keeps it permanent.
	if !IsPermanent(fmt.Errorf("attempt 3: %w", perm)) {
		t.Error("wrapped permanent error not detected")
	}
}
