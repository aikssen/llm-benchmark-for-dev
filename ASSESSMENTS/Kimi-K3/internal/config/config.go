// Package config loads service configuration from environment variables
// with sensible local defaults. Every knob is documented in the README.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime settings of the service.
type Config struct {
	// HTTP
	Port          int           // NOTIF_PORT
	ReadTimeout   time.Duration // NOTIF_HTTP_READ_TIMEOUT
	WriteTimeout  time.Duration // NOTIF_HTTP_WRITE_TIMEOUT
	IdleTimeout   time.Duration // NOTIF_HTTP_IDLE_TIMEOUT
	MaxRecipients int           // NOTIF_MAX_RECIPIENTS

	// Processing
	WorkerCount   int           // NOTIF_WORKERS
	QueueCapacity int           // NOTIF_QUEUE_CAPACITY
	ShutdownDrain time.Duration // NOTIF_SHUTDOWN_DRAIN

	// Delivery resilience
	ProviderTimeout  time.Duration // NOTIF_PROVIDER_TIMEOUT (per attempt)
	RetryMaxAttempts int           // NOTIF_RETRY_MAX_ATTEMPTS
	RetryBaseDelay   time.Duration // NOTIF_RETRY_BASE_DELAY
	RetryMaxDelay    time.Duration // NOTIF_RETRY_MAX_DELAY
	BreakerThreshold int           // NOTIF_BREAKER_THRESHOLD
	BreakerResetWait time.Duration // NOTIF_BREAKER_RESET_WAIT

	// Validation
	RateLimitPerWindow int           // NOTIF_RATE_LIMIT
	RateLimitWindow    time.Duration // NOTIF_RATE_LIMIT_WINDOW
	PrefsLatency       time.Duration // NOTIF_PREFS_LATENCY (simulated lookup cost)

	// Simulated providers
	SimMinLatency time.Duration // NOTIF_SIM_MIN_LATENCY
	SimMaxLatency time.Duration // NOTIF_SIM_MAX_LATENCY
	SimErrorRate  float64       // NOTIF_SIM_ERROR_RATE (0..1)
}

// Default returns the local-development configuration.
func Default() Config {
	return Config{
		Port:               8080,
		ReadTimeout:        5 * time.Second,
		WriteTimeout:       10 * time.Second,
		IdleTimeout:        60 * time.Second,
		MaxRecipients:      100,
		WorkerCount:        8,
		QueueCapacity:      1024,
		ShutdownDrain:      10 * time.Second,
		ProviderTimeout:    2 * time.Second,
		RetryMaxAttempts:   4,
		RetryBaseDelay:     100 * time.Millisecond,
		RetryMaxDelay:      2 * time.Second,
		BreakerThreshold:   5,
		BreakerResetWait:   5 * time.Second,
		RateLimitPerWindow: 10,
		RateLimitWindow:    time.Minute,
		PrefsLatency:       0,
		SimMinLatency:      20 * time.Millisecond,
		SimMaxLatency:      150 * time.Millisecond,
		SimErrorRate:       0.30,
	}
}

// Load overlays environment variables on top of Default.
func Load() (Config, error) {
	cfg := Default()
	var err error

	cfg.Port = envInt("NOTIF_PORT", cfg.Port, &err)
	cfg.ReadTimeout = envDuration("NOTIF_HTTP_READ_TIMEOUT", cfg.ReadTimeout, &err)
	cfg.WriteTimeout = envDuration("NOTIF_HTTP_WRITE_TIMEOUT", cfg.WriteTimeout, &err)
	cfg.IdleTimeout = envDuration("NOTIF_HTTP_IDLE_TIMEOUT", cfg.IdleTimeout, &err)
	cfg.MaxRecipients = envInt("NOTIF_MAX_RECIPIENTS", cfg.MaxRecipients, &err)
	cfg.WorkerCount = envInt("NOTIF_WORKERS", cfg.WorkerCount, &err)
	cfg.QueueCapacity = envInt("NOTIF_QUEUE_CAPACITY", cfg.QueueCapacity, &err)
	cfg.ShutdownDrain = envDuration("NOTIF_SHUTDOWN_DRAIN", cfg.ShutdownDrain, &err)
	cfg.ProviderTimeout = envDuration("NOTIF_PROVIDER_TIMEOUT", cfg.ProviderTimeout, &err)
	cfg.RetryMaxAttempts = envInt("NOTIF_RETRY_MAX_ATTEMPTS", cfg.RetryMaxAttempts, &err)
	cfg.RetryBaseDelay = envDuration("NOTIF_RETRY_BASE_DELAY", cfg.RetryBaseDelay, &err)
	cfg.RetryMaxDelay = envDuration("NOTIF_RETRY_MAX_DELAY", cfg.RetryMaxDelay, &err)
	cfg.BreakerThreshold = envInt("NOTIF_BREAKER_THRESHOLD", cfg.BreakerThreshold, &err)
	cfg.BreakerResetWait = envDuration("NOTIF_BREAKER_RESET_WAIT", cfg.BreakerResetWait, &err)
	cfg.RateLimitPerWindow = envInt("NOTIF_RATE_LIMIT", cfg.RateLimitPerWindow, &err)
	cfg.RateLimitWindow = envDuration("NOTIF_RATE_LIMIT_WINDOW", cfg.RateLimitWindow, &err)
	cfg.PrefsLatency = envDuration("NOTIF_PREFS_LATENCY", cfg.PrefsLatency, &err)
	cfg.SimMinLatency = envDuration("NOTIF_SIM_MIN_LATENCY", cfg.SimMinLatency, &err)
	cfg.SimMaxLatency = envDuration("NOTIF_SIM_MAX_LATENCY", cfg.SimMaxLatency, &err)
	cfg.SimErrorRate = envFloat("NOTIF_SIM_ERROR_RATE", cfg.SimErrorRate, &err)

	if err != nil {
		return Config{}, err
	}
	if cfg.SimErrorRate < 0 || cfg.SimErrorRate > 1 {
		return Config{}, fmt.Errorf("NOTIF_SIM_ERROR_RATE must be between 0 and 1, got %v", cfg.SimErrorRate)
	}
	if cfg.WorkerCount < 1 || cfg.QueueCapacity < 1 {
		return Config{}, fmt.Errorf("NOTIF_WORKERS and NOTIF_QUEUE_CAPACITY must be >= 1")
	}
	return cfg, nil
}

func envInt(key string, def int, err *error) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, e := strconv.Atoi(v)
	if e != nil {
		*err = fmt.Errorf("%s: %w", key, e)
		return def
	}
	return n
}

func envFloat(key string, def float64, err *error) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, e := strconv.ParseFloat(v, 64)
	if e != nil {
		*err = fmt.Errorf("%s: %w", key, e)
		return def
	}
	return f
}

func envDuration(key string, def time.Duration, err *error) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, e := time.ParseDuration(v)
	if e != nil {
		*err = fmt.Errorf("%s: %w", key, e)
		return def
	}
	return d
}
