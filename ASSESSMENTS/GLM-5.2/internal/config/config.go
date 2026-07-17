// Package config loads runtime configuration from the environment with sane
// defaults, so the service runs out of the box and is tunable in production.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	HTTPAddr string

	QueueCapacity        int
	Workers              int
	RecipientConcurrency int

	SendTimeout time.Duration

	RetryAttempts int
	RetryBase     time.Duration
	RetryMax      time.Duration

	CircuitFailures int
	CircuitCooldown time.Duration

	RateLimitWindow time.Duration
	RateLimitMax    int

	ListDefaultLimit int
	ListMaxLimit     int

	// Simulator provider tuning (demonstrates unreliability handling).
	ProviderFailureProb float64
	ProviderSlowProb    float64
	ProviderSlowDelay   time.Duration
	ProviderMinDelay    time.Duration

	LogLevel string
}

// Default returns a production-sensible default configuration.
func Default() Config {
	return Config{
		HTTPAddr:             ":8080",
		QueueCapacity:        1024,
		Workers:              8,
		RecipientConcurrency: 16,
		SendTimeout:          5 * time.Second,
		RetryAttempts:        3,
		RetryBase:            100 * time.Millisecond,
		RetryMax:             2 * time.Second,
		CircuitFailures:      5,
		CircuitCooldown:      10 * time.Second,
		RateLimitWindow:      1 * time.Minute,
		RateLimitMax:         60,
		ListDefaultLimit:     20,
		ListMaxLimit:         100,
		ProviderFailureProb:  0.1,
		ProviderSlowProb:     0.1,
		ProviderSlowDelay:    2 * time.Second,
		ProviderMinDelay:     20 * time.Millisecond,
		LogLevel:             "info",
	}
}

// FromEnv returns Default overridden by environment variables.
func FromEnv() Config {
	c := Default()
	c.HTTPAddr = envOr("HTTP_ADDR", c.HTTPAddr)
	c.QueueCapacity = envInt("QUEUE_CAPACITY", c.QueueCapacity)
	c.Workers = envInt("WORKERS", c.Workers)
	c.RecipientConcurrency = envInt("RECIPIENT_CONCURRENCY", c.RecipientConcurrency)
	c.SendTimeout = envDur("SEND_TIMEOUT", c.SendTimeout)
	c.RetryAttempts = envInt("RETRY_ATTEMPTS", c.RetryAttempts)
	c.RetryBase = envDur("RETRY_BASE", c.RetryBase)
	c.RetryMax = envDur("RETRY_MAX", c.RetryMax)
	c.CircuitFailures = envInt("CIRCUIT_FAILURES", c.CircuitFailures)
	c.CircuitCooldown = envDur("CIRCUIT_COOLDOWN", c.CircuitCooldown)
	c.RateLimitWindow = envDur("RATE_LIMIT_WINDOW", c.RateLimitWindow)
	c.RateLimitMax = envInt("RATE_LIMIT_MAX", c.RateLimitMax)
	c.ListDefaultLimit = envInt("LIST_DEFAULT_LIMIT", c.ListDefaultLimit)
	c.ListMaxLimit = envInt("LIST_MAX_LIMIT", c.ListMaxLimit)
	c.ProviderFailureProb = envFloat("PROVIDER_FAILURE_PROB", c.ProviderFailureProb)
	c.ProviderSlowProb = envFloat("PROVIDER_SLOW_PROB", c.ProviderSlowProb)
	c.ProviderSlowDelay = envDur("PROVIDER_SLOW_DELAY", c.ProviderSlowDelay)
	c.ProviderMinDelay = envDur("PROVIDER_MIN_DELAY", c.ProviderMinDelay)
	c.LogLevel = strings.ToLower(envOr("LOG_LEVEL", c.LogLevel))
	return c
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
