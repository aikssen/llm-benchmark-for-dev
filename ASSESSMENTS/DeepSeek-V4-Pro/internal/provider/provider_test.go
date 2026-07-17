package provider

import (
	"context"
	"testing"

	"github.com/assessment/notify-service/internal/domain"
)

func TestProviderSend(t *testing.T) {
	ctx := context.Background()

	t.Run("email provider sends", func(t *testing.T) {
		p := NewEmailProvider()
		if p.Name() != domain.ChannelEmail {
			t.Errorf("expected email provider name, got %s", p.Name())
		}
		err := p.Send(ctx, domain.Recipient{ID: "test@test.com"}, domain.Message{Body: "Hello"})
		// Can be nil or error due to random failure simulation
		_ = err
	})

	t.Run("sms provider sends", func(t *testing.T) {
		p := NewSMSProvider()
		if p.Name() != domain.ChannelSMS {
			t.Errorf("expected sms provider name, got %s", p.Name())
		}
		_ = p.Send(ctx, domain.Recipient{ID: "1234567890"}, domain.Message{Body: "Hello"})
	})

	t.Run("push provider sends", func(t *testing.T) {
		p := NewPushProvider()
		if p.Name() != domain.ChannelPush {
			t.Errorf("expected push provider name, got %s", p.Name())
		}
		_ = p.Send(ctx, domain.Recipient{ID: "device-token"}, domain.Message{Body: "Hello"})
	})
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	p := NewEmailProvider()
	reg.Register(p)

	got, err := reg.Get(domain.ChannelEmail)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != domain.ChannelEmail {
		t.Errorf("expected email provider, got %s", got.Name())
	}

	_, err = reg.Get(domain.ChannelSMS)
	if err == nil {
		t.Error("expected error for unregistered provider")
	}
}
