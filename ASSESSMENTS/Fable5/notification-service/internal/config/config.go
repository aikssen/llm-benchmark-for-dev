// Package config loads service configuration from environment variables with
// sensible defaults, so the service runs locally with zero configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration of the service.
type Config struct {
	// Addr is the HTTP listen address.
	Addr string
	// ShutdownTimeout bounds graceful HTTP shutdown.
	ShutdownTimeout time.Duration

	// Workers / queue.
	Workers       int
	QueueCapacity int

	// Delivery retries.
	MaxAttempts    int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration

	// Per-delivery processing timeouts.
	ValidationTimeout time.Duration
	SendTimeout       time.Duration

	// API limits.
	MaxRecipients   int
	MaxBodyBytes    int64
	DefaultPageSize int
	MaxPageSize     int

	// Rate limiting per (channel, recipient).
	RateLimit       int
	RateLimitWindow time.Duration

	// Simulated environment.
	ProviderMinLatency           time.Duration
	ProviderMaxLatency           time.Duration
	ProviderTransientFailureRate float64
	ProviderPermanentFailureRate float64
	// OptedOutRecipients simulates stored user preferences. Comma-separated
	// entries: "recipient" (all channels) or "channel:recipient".
	OptedOutRecipients []string
}

// Load reads configuration from the environment, applying defaults and
// validating values.
func Load() (Config, error) {
	cfg := Config{}
	var errs []error

	str := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	integer := func(key string, def, minVal int) int {
		v := os.Getenv(key)
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < minVal {
			errs = append(errs, fmt.Errorf("%s must be an integer >= %d, got %q", key, minVal, v))
			return def
		}
		return n
	}
	duration := func(key string, def time.Duration) time.Duration {
		v := os.Getenv(key)
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive duration (e.g. 500ms), got %q", key, v))
			return def
		}
		return d
	}
	rate := func(key string, def float64) float64 {
		v := os.Getenv(key)
		if v == "" {
			return def
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			errs = append(errs, fmt.Errorf("%s must be a number in [0,1], got %q", key, v))
			return def
		}
		return f
	}

	cfg.Addr = str("ADDR", ":8080")
	cfg.ShutdownTimeout = duration("SHUTDOWN_TIMEOUT", 10*time.Second)

	cfg.Workers = integer("WORKERS", 8, 1)
	cfg.QueueCapacity = integer("QUEUE_CAPACITY", 1024, 1)

	cfg.MaxAttempts = integer("MAX_ATTEMPTS", 4, 1)
	cfg.RetryBaseDelay = duration("RETRY_BASE_DELAY", 200*time.Millisecond)
	cfg.RetryMaxDelay = duration("RETRY_MAX_DELAY", 5*time.Second)

	cfg.ValidationTimeout = duration("VALIDATION_TIMEOUT", 2*time.Second)
	cfg.SendTimeout = duration("SEND_TIMEOUT", 5*time.Second)

	cfg.MaxRecipients = integer("MAX_RECIPIENTS", 100, 1)
	cfg.MaxBodyBytes = int64(integer("MAX_BODY_BYTES", 1<<20, 1))
	cfg.DefaultPageSize = integer("DEFAULT_PAGE_SIZE", 20, 1)
	cfg.MaxPageSize = integer("MAX_PAGE_SIZE", 100, 1)

	cfg.RateLimit = integer("RATE_LIMIT", 10, 1)
	cfg.RateLimitWindow = duration("RATE_LIMIT_WINDOW", time.Minute)

	cfg.ProviderMinLatency = duration("PROVIDER_MIN_LATENCY", 50*time.Millisecond)
	cfg.ProviderMaxLatency = duration("PROVIDER_MAX_LATENCY", 300*time.Millisecond)
	cfg.ProviderTransientFailureRate = rate("PROVIDER_TRANSIENT_FAILURE_RATE", 0.15)
	cfg.ProviderPermanentFailureRate = rate("PROVIDER_PERMANENT_FAILURE_RATE", 0.02)

	if v := os.Getenv("OPTED_OUT_RECIPIENTS"); v != "" {
		for entry := range strings.SplitSeq(v, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				cfg.OptedOutRecipients = append(cfg.OptedOutRecipients, entry)
			}
		}
	}

	if cfg.ProviderMaxLatency < cfg.ProviderMinLatency {
		errs = append(errs, fmt.Errorf("PROVIDER_MAX_LATENCY must be >= PROVIDER_MIN_LATENCY"))
	}
	if cfg.ProviderTransientFailureRate+cfg.ProviderPermanentFailureRate > 1 {
		errs = append(errs, fmt.Errorf("provider failure rates must sum to at most 1"))
	}
	if cfg.MaxPageSize < cfg.DefaultPageSize {
		errs = append(errs, fmt.Errorf("MAX_PAGE_SIZE must be >= DEFAULT_PAGE_SIZE"))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %v", errs)
	}
	return cfg, nil
}
