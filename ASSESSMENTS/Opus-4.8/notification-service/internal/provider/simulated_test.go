package provider

import (
	"context"
	"testing"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

func TestSimulated_AlwaysSucceeds(t *testing.T) {
	p := NewSimulated(domain.ChannelEmail, SimConfig{}, nil)
	if err := p.Send(context.Background(), &domain.Notification{ID: "1"}); err != nil {
		t.Fatalf("expected success with zero failure rate, got %v", err)
	}
	if p.Channel() != domain.ChannelEmail {
		t.Errorf("wrong channel: %s", p.Channel())
	}
}

func TestSimulated_AlwaysTransientFailure(t *testing.T) {
	p := NewSimulated(domain.ChannelSMS, SimConfig{FailureRate: 1.0}, nil)
	err := p.Send(context.Background(), &domain.Notification{ID: "1"})
	if err == nil {
		t.Fatal("expected failure with failure rate 1.0")
	}
	if !IsRetryable(err) {
		t.Error("transient failure should be retryable")
	}
}

func TestSimulated_AlwaysPermanentFailure(t *testing.T) {
	p := NewSimulated(domain.ChannelPush, SimConfig{PermanentRate: 1.0}, nil)
	err := p.Send(context.Background(), &domain.Notification{ID: "1"})
	if err == nil {
		t.Fatal("expected failure with permanent rate 1.0")
	}
	if IsRetryable(err) {
		t.Error("permanent failure should not be retryable")
	}
}

func TestSimulated_RespectsContextDeadline(t *testing.T) {
	// Latency far exceeds the deadline: Send must return promptly with a
	// retryable timeout rather than sleeping for the full latency.
	p := NewSimulated(domain.ChannelEmail, SimConfig{MinLatency: time.Second, MaxLatency: time.Second}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.Send(ctx, &domain.Notification{ID: "1"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !IsRetryable(err) {
		t.Error("timeout should be retryable")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("Send did not honour context deadline; took %v", time.Since(start))
	}
}
