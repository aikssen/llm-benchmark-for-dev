package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config contains the process-level controls that bound work under traffic bursts.
type Config struct {
	Address             string
	QueueCapacity       int
	NotificationWorkers int
	RecipientWorkers    int
	ValidationWorkers   int
	MaxRecipients       int
	MaxMessageBytes     int
	MaxAttempts         int
	AttemptTimeout      time.Duration
	RetryBaseDelay      time.Duration
}

func FromEnv() (Config, error) {
	if err := validateEnvironment(); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Address:             stringEnv("HTTP_ADDR", ":8080"),
		QueueCapacity:       intEnv("NOTIFICATION_QUEUE_CAPACITY", 256),
		NotificationWorkers: intEnv("NOTIFICATION_WORKERS", 4),
		RecipientWorkers:    intEnv("RECIPIENT_WORKERS", 8),
		ValidationWorkers:   intEnv("VALIDATION_WORKERS", 4),
		MaxRecipients:       intEnv("MAX_RECIPIENTS", 100),
		MaxMessageBytes:     intEnv("MAX_MESSAGE_BYTES", 10_000),
		MaxAttempts:         intEnv("MAX_DELIVERY_ATTEMPTS", 3),
		AttemptTimeout:      durationEnv("PROVIDER_ATTEMPT_TIMEOUT", 3*time.Second),
		RetryBaseDelay:      durationEnv("RETRY_BASE_DELAY", 100*time.Millisecond),
	}
	if cfg.QueueCapacity < 1 || cfg.NotificationWorkers < 1 || cfg.RecipientWorkers < 1 || cfg.ValidationWorkers < 1 || cfg.MaxRecipients < 1 || cfg.MaxMessageBytes < 1 || cfg.MaxAttempts < 1 || cfg.AttemptTimeout <= 0 || cfg.RetryBaseDelay <= 0 {
		return Config{}, fmt.Errorf("all numeric limits must be positive")
	}
	return cfg, nil
}

func validateEnvironment() error {
	integerNames := []string{
		"NOTIFICATION_QUEUE_CAPACITY", "NOTIFICATION_WORKERS", "RECIPIENT_WORKERS",
		"VALIDATION_WORKERS", "MAX_RECIPIENTS", "MAX_MESSAGE_BYTES", "MAX_DELIVERY_ATTEMPTS",
	}
	for _, name := range integerNames {
		if value := os.Getenv(name); value != "" {
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf("%s must be an integer: %w", name, err)
			}
		}
	}
	for _, name := range []string{"PROVIDER_ATTEMPT_TIMEOUT", "RETRY_BASE_DELAY"} {
		if value := os.Getenv(name); value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s must be a duration: %w", name, err)
			}
		}
	}
	return nil
}

func stringEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	if value := os.Getenv(name); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}
