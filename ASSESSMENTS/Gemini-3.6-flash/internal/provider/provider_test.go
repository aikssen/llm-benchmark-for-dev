package provider

import (
	"context"
	"errors"
	"testing"

	"notification-service/internal/domain"
	"notification-service/pkg/resilience"
)

type mockFailingProvider struct {
	id      string
	channel domain.Channel
}

func (m *mockFailingProvider) ID() string             { return m.id }
func (m *mockFailingProvider) Channel() domain.Channel { return m.channel }
func (m *mockFailingProvider) Send(ctx context.Context, item *domain.SingleRecipientItem) error {
	return errors.New("primary provider down")
}

func TestProviderRegistry_FallbackToSecondary(t *testing.T) {
	registry := NewRegistry()

	primary := &mockFailingProvider{id: "email-primary", channel: domain.ChannelEmail}
	secondary := NewSimulatedEmailProvider("email-secondary", 0.0, 0)

	noRetry := resilience.RetryConfig{MaxAttempts: 1}
	cbPrimary := resilience.NewCircuitBreaker(1, 1, 100)
	cbSecondary := resilience.NewCircuitBreaker(1, 1, 100)

	registry.Register(primary, noRetry, cbPrimary)
	registry.Register(secondary, noRetry, cbSecondary)

	item := &domain.SingleRecipientItem{
		Recipient: "user@example.com",
		Channel:   domain.ChannelEmail,
		Body:      "Test message",
	}

	providerUsed, err := registry.Send(context.Background(), item)
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}

	if providerUsed != "email-secondary" {
		t.Fatalf("expected provider 'email-secondary', got '%s'", providerUsed)
	}
}

func TestProviderRegistry_NoProvidersRegistered(t *testing.T) {
	registry := NewRegistry()
	item := &domain.SingleRecipientItem{
		Recipient: "user@example.com",
		Channel:   domain.ChannelEmail,
	}

	_, err := registry.Send(context.Background(), item)
	if !errors.Is(err, domain.ErrNoProviderAvailable) {
		t.Fatalf("expected ErrNoProviderAvailable, got %v", err)
	}
}
