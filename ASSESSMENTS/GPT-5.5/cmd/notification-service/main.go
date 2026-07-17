package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"notification-service/internal/api"
	"notification-service/internal/app"
	"notification-service/internal/domain"
	"notification-service/internal/processing"
	"notification-service/internal/provider"
	"notification-service/internal/store"
	"notification-service/internal/validation"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadConfig()

	repository := store.NewMemoryStore()
	providers := provider.NewRegistry()
	providers.Register(domain.ChannelEmail, provider.NewSimulatedSender(provider.SimulatedSenderConfig{
		Name:        "simulated-email",
		MinLatency:  80 * time.Millisecond,
		MaxLatency:  350 * time.Millisecond,
		FailureRate: cfg.ProviderFailureRate,
	}))
	providers.Register(domain.ChannelSMS, provider.NewSimulatedSender(provider.SimulatedSenderConfig{
		Name:        "simulated-sms",
		MinLatency:  100 * time.Millisecond,
		MaxLatency:  500 * time.Millisecond,
		FailureRate: cfg.ProviderFailureRate,
	}))
	providers.Register(domain.ChannelPush, provider.NewSimulatedSender(provider.SimulatedSenderConfig{
		Name:        "simulated-push",
		MinLatency:  40 * time.Millisecond,
		MaxLatency:  200 * time.Millisecond,
		FailureRate: cfg.ProviderFailureRate,
	}))

	validators := []validation.Validator{
		validation.RecipientValidator{},
		validation.MessageValidator{MaxBodyBytes: 4096},
		validation.PreferenceValidator{},
		validation.NewRateLimitValidator(10, time.Minute),
	}

	processor := processing.NewProcessor(repository, providers, validators, processing.Config{
		Workers:      cfg.Workers,
		QueueSize:    cfg.QueueSize,
		MaxAttempts:  3,
		SendTimeout:  2 * time.Second,
		RetryBackoff: 150 * time.Millisecond,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	processor.Start(ctx)
	defer processor.Stop()

	service := app.NewService(repository, processor, providers)
	handler := api.NewHandler(service)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("notification service listening", "addr", cfg.Addr, "workers", cfg.Workers, "queue_size", cfg.QueueSize)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

type config struct {
	Addr                string
	Workers             int
	QueueSize           int
	ProviderFailureRate float64
}

func loadConfig() config {
	return config{
		Addr:                getEnvString("ADDR", ":8080"),
		Workers:             getEnvInt("WORKERS", 8),
		QueueSize:           getEnvInt("QUEUE_SIZE", 1000),
		ProviderFailureRate: getEnvFloat("PROVIDER_FAILURE_RATE", 0.20),
	}
}

func getEnvString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func getEnvFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || parsed > 1 {
		return fallback
	}
	return parsed
}
