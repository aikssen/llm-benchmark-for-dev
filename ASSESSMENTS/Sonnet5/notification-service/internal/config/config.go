// Package config centralizes environment-driven configuration. Every value
// has a sane default so the service runs locally with zero setup.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all tunable parameters for the service.
type Config struct {
	// HTTP server
	Port string

	// Delivery pipeline
	WorkerPoolSize      int           // number of concurrent delivery workers
	QueueCapacity       int           // bounded channel size; caps in-flight backlog
	MaxDeliveryAttempts int           // per-notification attempts before giving up
	BaseBackoff         time.Duration // first retry delay
	MaxBackoff          time.Duration // retry delay ceiling
	ProviderTimeout     time.Duration // per-attempt context timeout

	// Circuit breaker (per channel/provider)
	BreakerFailureThreshold uint32        // consecutive failures to trip breaker
	BreakerOpenDuration     time.Duration // how long the breaker stays open

	// Rate limiting (recipient-level validation)
	RateLimitPerMinute int

	// Simulated provider behavior
	ProviderFailureRate float64 // 0..1 probability a send fails
	ProviderMinLatency  time.Duration
	ProviderMaxLatency  time.Duration
}

// Load reads configuration from the environment, falling back to defaults
// tuned for a responsive local/demo experience.
func Load() Config {
	return Config{
		Port: getEnv("PORT", "8080"),

		WorkerPoolSize:      getEnvInt("WORKER_POOL_SIZE", 16),
		QueueCapacity:       getEnvInt("QUEUE_CAPACITY", 1000),
		MaxDeliveryAttempts: getEnvInt("MAX_DELIVERY_ATTEMPTS", 4),
		BaseBackoff:         getEnvDuration("BASE_BACKOFF", 100*time.Millisecond),
		MaxBackoff:          getEnvDuration("MAX_BACKOFF", 2*time.Second),
		ProviderTimeout:     getEnvDuration("PROVIDER_TIMEOUT", 3*time.Second),

		BreakerFailureThreshold: uint32(getEnvInt("BREAKER_FAILURE_THRESHOLD", 5)),
		BreakerOpenDuration:     getEnvDuration("BREAKER_OPEN_DURATION", 5*time.Second),

		RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 60),

		ProviderFailureRate: getEnvFloat("PROVIDER_FAILURE_RATE", 0.2),
		ProviderMinLatency:  getEnvDuration("PROVIDER_MIN_LATENCY", 20*time.Millisecond),
		ProviderMaxLatency:  getEnvDuration("PROVIDER_MAX_LATENCY", 150*time.Millisecond),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
