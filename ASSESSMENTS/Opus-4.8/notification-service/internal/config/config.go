// Package config loads runtime configuration from environment variables with
// documented defaults. Everything the service needs to run has a sane default,
// so it starts with zero configuration; env vars override for tuning and
// deployment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the fully-resolved service configuration.
type Config struct {
	// HTTP
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	SubmitTimeout   time.Duration // max time a Submit may block on a full queue

	// Worker pool
	Workers   int
	QueueSize int

	// Delivery
	MaxAttempts    int
	BaseBackoff    time.Duration
	MaxBackoff     time.Duration
	AttemptTimeout time.Duration
	CBMaxFailures  int
	CBCooldown     time.Duration

	// Rate limiting
	RateLimit       int
	RateLimitWindow time.Duration

	// Simulated providers
	ProviderFailureRate   float64
	ProviderPermanentRate float64
	ProviderMinLatency    time.Duration
	ProviderMaxLatency    time.Duration
}

// Default returns the baseline configuration used when no env vars are set.
func Default() Config {
	return Config{
		Addr:            ":8080",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    10 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		SubmitTimeout:   2 * time.Second,

		Workers:   8,
		QueueSize: 1024,

		MaxAttempts:    4,
		BaseBackoff:    100 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		AttemptTimeout: 3 * time.Second,
		CBMaxFailures:  5,
		CBCooldown:     10 * time.Second,

		RateLimit:       20,
		RateLimitWindow: time.Minute,

		ProviderFailureRate:   0.2,
		ProviderPermanentRate: 0.02,
		ProviderMinLatency:    10 * time.Millisecond,
		ProviderMaxLatency:    150 * time.Millisecond,
	}
}

// Load builds a Config from Default() overlaid with environment variables. It
// returns an error if a provided value is malformed, failing fast at startup
// rather than surfacing confusing behaviour later.
func Load() (Config, error) {
	c := Default()
	var err error

	c.Addr = env("NS_ADDR", c.Addr)
	if c.ReadTimeout, err = envDuration("NS_READ_TIMEOUT", c.ReadTimeout); err != nil {
		return c, err
	}
	if c.WriteTimeout, err = envDuration("NS_WRITE_TIMEOUT", c.WriteTimeout); err != nil {
		return c, err
	}
	if c.ShutdownTimeout, err = envDuration("NS_SHUTDOWN_TIMEOUT", c.ShutdownTimeout); err != nil {
		return c, err
	}
	if c.SubmitTimeout, err = envDuration("NS_SUBMIT_TIMEOUT", c.SubmitTimeout); err != nil {
		return c, err
	}
	if c.Workers, err = envInt("NS_WORKERS", c.Workers); err != nil {
		return c, err
	}
	if c.QueueSize, err = envInt("NS_QUEUE_SIZE", c.QueueSize); err != nil {
		return c, err
	}
	if c.MaxAttempts, err = envInt("NS_MAX_ATTEMPTS", c.MaxAttempts); err != nil {
		return c, err
	}
	if c.BaseBackoff, err = envDuration("NS_BASE_BACKOFF", c.BaseBackoff); err != nil {
		return c, err
	}
	if c.MaxBackoff, err = envDuration("NS_MAX_BACKOFF", c.MaxBackoff); err != nil {
		return c, err
	}
	if c.AttemptTimeout, err = envDuration("NS_ATTEMPT_TIMEOUT", c.AttemptTimeout); err != nil {
		return c, err
	}
	if c.CBMaxFailures, err = envInt("NS_CB_MAX_FAILURES", c.CBMaxFailures); err != nil {
		return c, err
	}
	if c.CBCooldown, err = envDuration("NS_CB_COOLDOWN", c.CBCooldown); err != nil {
		return c, err
	}
	if c.RateLimit, err = envInt("NS_RATE_LIMIT", c.RateLimit); err != nil {
		return c, err
	}
	if c.RateLimitWindow, err = envDuration("NS_RATE_LIMIT_WINDOW", c.RateLimitWindow); err != nil {
		return c, err
	}
	if c.ProviderFailureRate, err = envFloat("NS_PROVIDER_FAILURE_RATE", c.ProviderFailureRate); err != nil {
		return c, err
	}
	if c.ProviderPermanentRate, err = envFloat("NS_PROVIDER_PERMANENT_RATE", c.ProviderPermanentRate); err != nil {
		return c, err
	}
	if c.ProviderMinLatency, err = envDuration("NS_PROVIDER_MIN_LATENCY", c.ProviderMinLatency); err != nil {
		return c, err
	}
	if c.ProviderMaxLatency, err = envDuration("NS_PROVIDER_MAX_LATENCY", c.ProviderMaxLatency); err != nil {
		return c, err
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}

func envFloat(key string, def float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def, fmt.Errorf("invalid %s: %w", key, err)
	}
	return f, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def, fmt.Errorf("invalid %s: %w", key, err)
	}
	return d, nil
}
