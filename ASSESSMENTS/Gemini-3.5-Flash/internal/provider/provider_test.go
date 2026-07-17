package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"notification-service/internal/domain"
)

func TestProviderRegistry(t *testing.T) {
	registry := NewRegistry()

	// 1. Test empty registry
	_, err := registry.GetProviders(domain.ChannelEmail)
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got: %v", err)
	}

	// 2. Register email providers
	p1 := NewMockProvider("SendGrid", domain.ChannelEmail, 0.0, 0)
	p2 := NewMockProvider("Mailgun", domain.ChannelEmail, 0.0, 0)
	registry.Register(p1)
	registry.Register(p2)

	// 3. Get registered providers
	provs, err := registry.GetProviders(domain.ChannelEmail)
	if err != nil {
		t.Fatalf("failed to get providers: %v", err)
	}

	if len(provs) != 2 {
		t.Errorf("expected 2 providers, got %d", len(provs))
	}

	if provs[0].Name() != "SendGrid" || provs[1].Name() != "Mailgun" {
		t.Errorf("providers ordered incorrectly or mismatch names: %v, %v", provs[0].Name(), provs[1].Name())
	}

	// 4. Thread safety & mutation safety test
	provs[0] = NewMockProvider("Mutated", domain.ChannelEmail, 0.0, 0)
	provsAfter, _ := registry.GetProviders(domain.ChannelEmail)
	if provsAfter[0].Name() == "Mutated" {
		t.Error("registry returned slice was mutable; expected deep-copied slice")
	}
}

func TestMockProvider_Send(t *testing.T) {
	ctx := context.Background()

	// 1. Test success (failRate = 0.0)
	pSuccess := NewMockProvider("TestSuccess", domain.ChannelSMS, 0.0, 1*time.Millisecond)
	n := &domain.Notification{
		ID:        "n-123",
		Recipient: "+15551234567",
		Channel:   domain.ChannelSMS,
		Content:   "test content",
	}

	err := pSuccess.Send(ctx, n)
	if err != nil {
		t.Errorf("expected mock send success, got: %v", err)
	}

	// 2. Test context cancellation
	pSlow := NewMockProvider("TestSlow", domain.ChannelSMS, 0.0, 100*time.Millisecond)
	cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err = pSlow.Send(cancelCtx, n)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context DeadlineExceeded, got: %v", err)
	}
}
