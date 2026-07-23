package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                   string
	WorkerPoolSize         int
	WorkerQueueCapacity    int
	RateLimitMaxRequests   int
	RateLimitWindowSeconds time.Duration
}

func Load() *Config {
	return &Config{
		Port:                   getEnv("PORT", "8080"),
		WorkerPoolSize:         getEnvAsInt("WORKER_POOL_SIZE", 20),
		WorkerQueueCapacity:    getEnvAsInt("WORKER_QUEUE_CAPACITY", 10000),
		RateLimitMaxRequests:   getEnvAsInt("RATE_LIMIT_MAX_REQUESTS", 100),
		RateLimitWindowSeconds: time.Duration(getEnvAsInt("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second,
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	if valStr, ok := os.LookupEnv(key); ok {
		if val, err := strconv.Atoi(valStr); err == nil {
			return val
		}
	}
	return defaultVal
}
