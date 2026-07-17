package config

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("unexpected default port: %d", cfg.Port)
	}
	if cfg.WorkerCount < 1 || cfg.QueueCapacity < 1 {
		t.Fatal("defaults must be runnable")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("NOTIF_PORT", "9999")
	t.Setenv("NOTIF_WORKERS", "42")
	t.Setenv("NOTIF_SIM_ERROR_RATE", "0.5")
	t.Setenv("NOTIF_PROVIDER_TIMEOUT", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9999 || cfg.WorkerCount != 42 {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
	if cfg.SimErrorRate != 0.5 {
		t.Fatalf("float override not applied: %v", cfg.SimErrorRate)
	}
	if cfg.ProviderTimeout != 3*time.Second {
		t.Fatalf("duration override not applied: %v", cfg.ProviderTimeout)
	}
}

func TestInvalidEnvFails(t *testing.T) {
	t.Setenv("NOTIF_PORT", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid NOTIF_PORT")
	}
}

func TestInvalidErrorRateFails(t *testing.T) {
	t.Setenv("NOTIF_SIM_ERROR_RATE", "1.5")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for out-of-range error rate")
	}
}
