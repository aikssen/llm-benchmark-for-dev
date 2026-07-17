package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds runtime configuration for the service.
type Config struct {
	HTTPAddr            string
	WorkerCount         int
	QueueSize           int
	MaxRetries          int
	RetryBaseDelay      time.Duration
	ProviderFailRate    float64
	ProviderMinLatency  time.Duration
	ProviderMaxLatency  time.Duration
	RateLimitPerMinute  int
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		HTTPAddr:           getEnv("HTTP_ADDR", ":8080"),
		WorkerCount:        getEnvInt("WORKER_COUNT", 8),
		QueueSize:          getEnvInt("QUEUE_SIZE", 256),
		MaxRetries:         getEnvInt("MAX_RETRIES", 3),
		RetryBaseDelay:     getEnvDuration("RETRY_BASE_DELAY", 200*time.Millisecond),
		ProviderFailRate:   getEnvFloat("PROVIDER_FAIL_RATE", 0.2),
		ProviderMinLatency: getEnvDuration("PROVIDER_MIN_LATENCY", 20*time.Millisecond),
		ProviderMaxLatency: getEnvDuration("PROVIDER_MAX_LATENCY", 150*time.Millisecond),
		RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 60),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
