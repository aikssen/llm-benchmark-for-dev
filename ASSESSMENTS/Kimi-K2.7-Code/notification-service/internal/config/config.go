package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds application configuration.
type Config struct {
	HTTPPort      string
	Workers       int
	QueueSize     int
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	ShutdownTimeout time.Duration
}

// Load builds Config from environment variables with sensible defaults.
func Load() Config {
	return Config{
		HTTPPort:        getEnv("HTTP_PORT", "8080"),
		Workers:         getEnvInt("WORKERS", 4),
		QueueSize:       getEnvInt("QUEUE_SIZE", 1000),
		ReadTimeout:     getEnvDuration("READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    getEnvDuration("WRITE_TIMEOUT", 10*time.Second),
		ShutdownTimeout: getEnvDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

// Addr returns the HTTP listen address.
func (c Config) Addr() string {
	return fmt.Sprintf(":%s", c.HTTPPort)
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
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
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
