package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/config"
)

// The service must be runnable with no configuration at all.
func TestLoadWithNoEnvironmentReturnsWorkingDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v, want the defaults to be valid", err)
	}
	if cfg.Server.Addr == "" || cfg.Worker.Count < 1 || cfg.Worker.QueueCapacity < 1 {
		t.Errorf("defaults are not usable: %+v", cfg)
	}
	if cfg.Limits.MaxRecipients > cfg.Worker.QueueCapacity {
		t.Error("the default request limit exceeds the default queue capacity")
	}
}

func TestLoadReadsTheEnvironment(t *testing.T) {
	t.Setenv("SERVER_ADDR", ":9999")
	t.Setenv("WORKER_COUNT", "32")
	t.Setenv("QUEUE_CAPACITY", "8192")
	t.Setenv("PROVIDER_CALL_TIMEOUT", "750ms")
	t.Setenv("RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("RETRY_MULTIPLIER", "1.5")
	t.Setenv("PROVIDER_TRANSIENT_FAILURE_RATE", "0.25")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "text")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"SERVER_ADDR", cfg.Server.Addr, ":9999"},
		{"WORKER_COUNT", cfg.Worker.Count, 32},
		{"QUEUE_CAPACITY", cfg.Worker.QueueCapacity, 8192},
		{"PROVIDER_CALL_TIMEOUT", cfg.Provider.CallTimeout, 750 * time.Millisecond},
		{"RETRY_MAX_ATTEMPTS", cfg.Retry.MaxAttempts, 5},
		{"RETRY_MULTIPLIER", cfg.Retry.Multiplier, 1.5},
		{"PROVIDER_TRANSIENT_FAILURE_RATE", cfg.Provider.TransientFailureRate, 0.25},
		{"LOG_LEVEL", cfg.Log.Level, "debug"},
		{"LOG_FORMAT", cfg.Log.Format, "text"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadIgnoresBlankValues(t *testing.T) {
	t.Setenv("SERVER_ADDR", "   ")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.Server.Addr != config.Default().Server.Addr {
		t.Errorf("Addr = %q, want the default for a blank value", cfg.Server.Addr)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"non-numeric worker count", map[string]string{"WORKER_COUNT": "many"}},
		{"worker count below the minimum", map[string]string{"WORKER_COUNT": "0"}},
		{"malformed duration", map[string]string{"PROVIDER_CALL_TIMEOUT": "3 seconds"}},
		{"negative duration", map[string]string{"PROVIDER_CALL_TIMEOUT": "-1s"}},
		{"failure rate above one", map[string]string{"PROVIDER_TRANSIENT_FAILURE_RATE": "1.5"}},
		{"unknown log format", map[string]string{"LOG_FORMAT": "xml"}},
		{"unknown log level", map[string]string{"LOG_LEVEL": "verbose"}},
		{
			name: "failure rates summing above one",
			env: map[string]string{
				"PROVIDER_TRANSIENT_FAILURE_RATE": "0.7",
				"PROVIDER_PERMANENT_FAILURE_RATE": "0.5",
			},
		},
		{
			name: "max latency below min latency",
			env:  map[string]string{"PROVIDER_MIN_LATENCY": "5s", "PROVIDER_MAX_LATENCY": "1s"},
		},
		{
			name: "max backoff below initial backoff",
			env:  map[string]string{"RETRY_INITIAL_BACKOFF": "10s", "RETRY_MAX_BACKOFF": "1s"},
		},
		{
			// Requests this large could never be admitted, so this is caught
			// at start-up rather than surfacing as a confusing 400 later.
			name: "recipient limit above the queue capacity",
			env:  map[string]string{"MAX_RECIPIENTS_PER_REQUEST": "5000", "QUEUE_CAPACITY": "100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if _, err := config.Load(); err == nil {
				t.Fatal("Load() = nil, want an error")
			}
		})
	}
}

// An operator should not have to restart once per typo.
func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	t.Setenv("WORKER_COUNT", "zero")
	t.Setenv("LOG_FORMAT", "xml")
	t.Setenv("PROVIDER_CALL_TIMEOUT", "soon")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() = nil, want an error")
	}
	for _, key := range []string{"WORKER_COUNT", "LOG_FORMAT", "PROVIDER_CALL_TIMEOUT"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the error does not mention %s: %v", key, err)
		}
	}
}
