package provider

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"notificationservice/internal/domain"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// fixedRand returns a Simulated provider whose random rolls are fixed, making
// outcomes deterministic.
func simWithRoll(cfg SimulatedConfig, roll float64) *Simulated {
	s := NewSimulated(domain.ChannelEmail, cfg, discardLogger())
	s.rand = func() float64 { return roll }
	return s
}

func TestSimulatedOutcomes(t *testing.T) {
	cfg := SimulatedConfig{PermanentFailureRate: 0.1, TransientFailureRate: 0.3}
	n := domain.Notification{ID: "ntf_1"}

	t.Run("permanent failure", func(t *testing.T) {
		err := simWithRoll(cfg, 0.05).Send(context.Background(), n, "a@b.com")
		if err == nil || !IsPermanent(err) {
			t.Errorf("expected permanent error, got %v", err)
		}
	})

	t.Run("transient failure", func(t *testing.T) {
		err := simWithRoll(cfg, 0.2).Send(context.Background(), n, "a@b.com")
		if err == nil || IsPermanent(err) {
			t.Errorf("expected transient error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		if err := simWithRoll(cfg, 0.9).Send(context.Background(), n, "a@b.com"); err != nil {
			t.Errorf("expected success, got %v", err)
		}
	})
}

func TestSimulatedHonorsContextDuringLatency(t *testing.T) {
	s := simWithRoll(SimulatedConfig{MinLatency: time.Minute, MaxLatency: time.Minute}, 0.9)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := s.Send(ctx, domain.Notification{}, "a@b.com")
	if err == nil {
		t.Fatal("expected error when context expires mid-send")
	}
	if IsPermanent(err) {
		t.Error("cancellation must be transient (retryable)")
	}
	if time.Since(start) > time.Second {
		t.Error("Send did not abort promptly on context expiry")
	}
}
