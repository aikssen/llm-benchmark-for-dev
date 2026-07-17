package provider

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/notifications-service/internal/domain"
)

func TestSimulatedProvider_Channel(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		channel domain.Channel
	}{
		{domain.ChannelEmail},
		{domain.ChannelSMS},
		{domain.ChannelPush},
	}

	for _, tt := range tests {
		p := NewSimulatedProvider(tt.channel, logger)
		if p.Channel() != tt.channel {
			t.Errorf("Channel() = %v, want %v", p.Channel(), tt.channel)
		}
	}
}

func TestSimulatedProvider_Send(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	p := NewSimulatedProvider(domain.ChannelEmail, logger)
	p.failRate = 0

	ctx := context.Background()
	err := p.Send(ctx, "user@example.com", "Test", "Hello")
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}
}

func TestSimulatedProvider_SendContextCancelled(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	p := NewSimulatedProvider(domain.ChannelEmail, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Send(ctx, "user@example.com", "Test", "Hello")
	if err == nil {
		t.Error("Send() should fail with cancelled context")
	}
}

func TestRegistry(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	registry := NewRegistry()

	emailProvider := NewSimulatedProvider(domain.ChannelEmail, logger)
	registry.Register(emailProvider)

	p, ok := registry.Get(domain.ChannelEmail)
	if !ok {
		t.Error("Get() should find registered provider")
	}
	if p.Channel() != domain.ChannelEmail {
		t.Errorf("Get() channel = %v, want %v", p.Channel(), domain.ChannelEmail)
	}

	_, ok = registry.Get(domain.ChannelSMS)
	if ok {
		t.Error("Get() should not find unregistered provider")
	}
}
