package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	ShutdownTimeout     time.Duration
	WorkerCount         int
	QueueSize           int
	MaxAttempts         int
	BaseBackoff         time.Duration
	MaxBackoff          time.Duration
	PerAttemptTimeout   time.Duration
	RateLimitCapacity   int
	RateLimitRefillPerS float64
	SweeperInterval     time.Duration
	SweeperTTL          time.Duration

	SimulatedProvider struct {
		Enabled         bool
		LatencyMin      time.Duration
		LatencyMax      time.Duration
		FailureRate     float64
		UnavailableRate float64
		Seed            int64
	}
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:            getEnv("HTTP_ADDR", ":8080"),
		ShutdownTimeout:     getDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		WorkerCount:         getInt("WORKER_COUNT", 8),
		QueueSize:           getInt("QUEUE_SIZE", 256),
		MaxAttempts:         getInt("MAX_ATTEMPTS", 4),
		BaseBackoff:         getDuration("BASE_BACKOFF", 100*time.Millisecond),
		MaxBackoff:          getDuration("MAX_BACKOFF", 2*time.Second),
		PerAttemptTimeout:   getDuration("PER_ATTEMPT_TIMEOUT", 5*time.Second),
		RateLimitCapacity:   getInt("RATE_LIMIT_CAPACITY", 5),
		RateLimitRefillPerS: getFloat("RATE_LIMIT_REFILL_PER_SEC", 1),
		SweeperInterval:     getDuration("SWEEPER_INTERVAL", 5*time.Minute),
		SweeperTTL:          getDuration("SWEEPER_TTL", 24*time.Hour),
	}
	cfg.SimulatedProvider.Enabled = getBool("SIMULATED_PROVIDER_ENABLED", true)
	cfg.SimulatedProvider.LatencyMin = getDuration("PROVIDER_LATENCY_MIN", 5*time.Millisecond)
	cfg.SimulatedProvider.LatencyMax = getDuration("PROVIDER_LATENCY_MAX", 50*time.Millisecond)
	cfg.SimulatedProvider.FailureRate = getFloat("PROVIDER_FAILURE_RATE", 0.2)
	cfg.SimulatedProvider.UnavailableRate = getFloat("PROVIDER_UNAVAILABLE_RATE", 0.05)
	cfg.SimulatedProvider.Seed = int64(getInt("PROVIDER_SEED", 42))

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.WorkerCount <= 0 {
		return fmt.Errorf("WORKER_COUNT must be > 0")
	}
	if c.QueueSize <= 0 {
		return fmt.Errorf("QUEUE_SIZE must be > 0")
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("MAX_ATTEMPTS must be >= 1")
	}
	if c.PerAttemptTimeout <= 0 {
		return fmt.Errorf("PER_ATTEMPT_TIMEOUT must be > 0")
	}
	if c.SimulatedProvider.LatencyMax < c.SimulatedProvider.LatencyMin {
		return fmt.Errorf("PROVIDER_LATENCY_MAX must be >= PROVIDER_LATENCY_MIN")
	}
	return nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on":
			return true
		case "0", "false", "no", "n", "off":
			return false
		}
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
