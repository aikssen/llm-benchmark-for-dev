package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	Workers           int
	QueueCapacity     int
	MaxRecipients     int
	MaxBodyBytes      int
	MaxSubjectBytes   int
	ProviderTimeout   time.Duration
	ProviderLatency   time.Duration
	ProviderFailure   float64
	RetryMaxAttempts  int
	RetryBaseDelay    time.Duration
	RetryMaxDelay     time.Duration
	RateLimit         int
	RateLimitWindow   time.Duration
	BlockedRecipients []string
	ShutdownTimeout   time.Duration
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:          envString("HTTP_ADDR", ":8080"),
		Workers:           envInt("WORKER_COUNT", 8),
		QueueCapacity:     envInt("QUEUE_CAPACITY", 1000),
		MaxRecipients:     envInt("MAX_RECIPIENTS", 100),
		MaxBodyBytes:      envInt("MAX_MESSAGE_BYTES", 4096),
		MaxSubjectBytes:   envInt("MAX_SUBJECT_BYTES", 256),
		ProviderTimeout:   envDuration("PROVIDER_TIMEOUT", 2*time.Second),
		ProviderLatency:   envDuration("SIMULATED_PROVIDER_LATENCY", 50*time.Millisecond),
		ProviderFailure:   envFloat("SIMULATED_FAILURE_RATE", 0.05),
		RetryMaxAttempts:  envInt("RETRY_MAX_ATTEMPTS", 3),
		RetryBaseDelay:    envDuration("RETRY_BASE_DELAY", 100*time.Millisecond),
		RetryMaxDelay:     envDuration("RETRY_MAX_DELAY", 2*time.Second),
		RateLimit:         envInt("RECIPIENT_RATE_LIMIT", 20),
		RateLimitWindow:   envDuration("RATE_LIMIT_WINDOW", time.Minute),
		BlockedRecipients: split(envString("BLOCKED_RECIPIENTS", "")),
		ShutdownTimeout:   envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	if config.Workers <= 0 || config.QueueCapacity <= 0 || config.MaxRecipients <= 0 {
		return Config{}, fmt.Errorf("WORKER_COUNT, QUEUE_CAPACITY, and MAX_RECIPIENTS must be positive")
	}
	if config.MaxRecipients > config.QueueCapacity {
		return Config{}, fmt.Errorf("MAX_RECIPIENTS must not exceed QUEUE_CAPACITY")
	}
	if config.MaxBodyBytes <= 0 || config.MaxSubjectBytes <= 0 || config.RateLimit <= 0 {
		return Config{}, fmt.Errorf("message limits and RECIPIENT_RATE_LIMIT must be positive")
	}
	if config.ProviderFailure < 0 || config.ProviderFailure > 1 {
		return Config{}, fmt.Errorf("SIMULATED_FAILURE_RATE must be between 0 and 1")
	}
	if config.RetryMaxAttempts <= 0 || config.RetryBaseDelay <= 0 || config.RetryMaxDelay < config.RetryBaseDelay {
		return Config{}, fmt.Errorf("retry configuration is invalid")
	}
	if config.ProviderTimeout <= 0 || config.RateLimitWindow <= 0 || config.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("timeout and window durations must be positive")
	}
	if config.ProviderLatency < 0 {
		return Config{}, fmt.Errorf("SIMULATED_PROVIDER_LATENCY must not be negative")
	}
	return config, nil
}

func envString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return -1
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return parsed
}

func split(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}
