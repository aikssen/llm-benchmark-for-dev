package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/provider"
)

// stubProvider is the minimum a new channel integration has to write, which
// is itself a check that the interface stays small.
type stubProvider struct {
	name    string
	channel domain.Channel
}

func (s stubProvider) Name() string                 { return s.name }
func (s stubProvider) Channel() domain.Channel      { return s.channel }
func (s stubProvider) ValidateAddress(string) error { return nil }
func (s stubProvider) Send(context.Context, provider.SendRequest) (provider.SendResult, error) {
	return provider.SendResult{MessageID: "stub-1"}, nil
}

func TestRegistryResolvesByChannel(t *testing.T) {
	t.Parallel()

	r := provider.NewRegistry()
	email := stubProvider{name: "email-provider", channel: domain.ChannelEmail}
	if err := r.Register(email); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	got, err := r.Get(domain.ChannelEmail)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.Name() != "email-provider" {
		t.Errorf("Get() returned %q, want %q", got.Name(), "email-provider")
	}
	if !r.Supports(domain.ChannelEmail) {
		t.Error("Supports(email) = false after registering an email provider")
	}
}

func TestRegistryReportsUnknownChannels(t *testing.T) {
	t.Parallel()

	r := provider.NewRegistry()
	if _, err := r.Get(domain.ChannelSMS); !errors.Is(err, provider.ErrNoProvider) {
		t.Errorf("Get() for an unregistered channel = %v, want ErrNoProvider", err)
	}
	if r.Supports(domain.ChannelSMS) {
		t.Error("Supports() = true for an unregistered channel")
	}
}

// Silently overwriting would make a wiring mistake invisible until messages
// started going to the wrong provider in production.
func TestRegistryRefusesDuplicateChannels(t *testing.T) {
	t.Parallel()

	r := provider.NewRegistry()
	if err := r.Register(stubProvider{name: "first", channel: domain.ChannelEmail}); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := r.Register(stubProvider{name: "second", channel: domain.ChannelEmail}); err == nil {
		t.Fatal("Register() accepted a second provider for the same channel")
	}

	got, _ := r.Get(domain.ChannelEmail)
	if got.Name() != "first" {
		t.Errorf("the original provider was replaced by %q", got.Name())
	}
}

func TestRegistryRejectsInvalidProviders(t *testing.T) {
	t.Parallel()

	r := provider.NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("Register(nil) = nil, want an error")
	}
	if err := r.Register(stubProvider{name: "no-channel"}); err == nil {
		t.Error("Register() accepted a provider with no channel")
	}
}

// Adding a channel must not require touching anything but the registration.
func TestRegistrySupportsChannelsBeyondTheBuiltInSet(t *testing.T) {
	t.Parallel()

	webhook := domain.Channel("webhook")
	r := provider.NewRegistry()
	if err := r.Register(stubProvider{name: "webhook-provider", channel: webhook}); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if !r.Supports(webhook) {
		t.Error("a channel outside the predeclared constants was not supported")
	}
}

func TestRegistryChannelsAreSorted(t *testing.T) {
	t.Parallel()

	r := provider.NewRegistry()
	r.MustRegister(
		stubProvider{name: "sms", channel: domain.ChannelSMS},
		stubProvider{name: "email", channel: domain.ChannelEmail},
		stubProvider{name: "push", channel: domain.ChannelPush},
	)

	want := []domain.Channel{domain.ChannelEmail, domain.ChannelPush, domain.ChannelSMS}
	got := r.Channels()
	if len(got) != len(want) {
		t.Fatalf("Channels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Channels() = %v, want %v", got, want)
		}
	}
}

func TestProviderErrorClassification(t *testing.T) {
	t.Parallel()

	transient := provider.Transient("p", provider.CodeUnavailable, "down")
	if !transient.Retryable() {
		t.Error("Transient() produced a non-retryable error")
	}
	permanent := provider.Permanent("p", provider.CodeRejected, "bad address")
	if permanent.Retryable() {
		t.Error("Permanent() produced a retryable error")
	}

	// The concrete type must survive wrapping so callers can classify it.
	wrapped := errors.Join(errors.New("dispatching"), transient)
	var perr *provider.Error
	if !errors.As(wrapped, &perr) || perr.Code != provider.CodeUnavailable {
		t.Errorf("errors.As could not recover the provider error from %v", wrapped)
	}
}
