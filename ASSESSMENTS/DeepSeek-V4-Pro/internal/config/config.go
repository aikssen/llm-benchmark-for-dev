package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerPort       string
	MaxWorkers       int
	MaxRetries       int
	RetryBackoff     time.Duration
	RequestBuffer    int
	RateLimit        int
	RateLimitWindow  time.Duration
}

func Load() *Config {
	return &Config{
		ServerPort:      envOrDefault("SERVER_PORT", "8080"),
		MaxWorkers:      envIntOrDefault("MAX_WORKERS", 10),
		MaxRetries:       envIntOrDefault("MAX_RETRIES", 3),
		RetryBackoff:    envDurationOrDefault("RETRY_BACKOFF_MS", 300) * time.Millisecond,
		RequestBuffer:   envIntOrDefault("REQUEST_BUFFER", 100),
		RateLimit:       envIntOrDefault("RATE_LIMIT", 5),
		RateLimitWindow: envDurationOrDefault("RATE_LIMIT_WINDOW_SEC", 60) * time.Second,
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envIntOrDefault(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if iv, err := strconv.Atoi(v); err == nil {
			return iv
		}
	}
	return defaultVal
}

func envDurationOrDefault(key string, defaultVal int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if iv, err := strconv.Atoi(v); err == nil {
			return time.Duration(iv)
		}
	}
	return time.Duration(defaultVal)
}
