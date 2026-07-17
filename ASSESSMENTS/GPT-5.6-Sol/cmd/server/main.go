package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"notification/internal/config"
	"notification/internal/httpapi"
	"notification/internal/memory"
	"notification/internal/notification"
	"notification/internal/provider"
	"notification/internal/service"
	"notification/internal/validation"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := config.Load()
	if err != nil {
		return err
	}

	repository := memory.NewRepository()
	providers := provider.Registry{
		notification.ChannelEmail: provider.NewSimulated("email", config.ProviderLatency, config.ProviderFailure, 11),
		notification.ChannelSMS:   provider.NewSimulated("sms", config.ProviderLatency, config.ProviderFailure, 22),
		notification.ChannelPush:  provider.NewSimulated("push", config.ProviderLatency, config.ProviderFailure, 33),
	}
	validators := []notification.Validator{
		validation.Recipient{},
		validation.Message{MaxSubjectBytes: config.MaxSubjectBytes},
		validation.NewPreferences(config.BlockedRecipients),
		validation.NewRateLimit(config.RateLimit, config.RateLimitWindow),
	}
	processor := service.NewProcessor(repository, providers, validators, service.RetryPolicy{
		MaxAttempts: config.RetryMaxAttempts,
		BaseDelay:   config.RetryBaseDelay,
		MaxDelay:    config.RetryMaxDelay,
		Timeout:     config.ProviderTimeout,
	})
	notifications := service.New(repository, processor, service.Config{
		Workers:       config.Workers,
		QueueCapacity: config.QueueCapacity,
		MaxRecipients: config.MaxRecipients,
		MaxBodyBytes:  config.MaxBodyBytes,
	})

	server := &http.Server{
		Addr:              config.HTTPAddr,
		Handler:           httpapi.New(notifications, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "address", config.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	select {
	case err = <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-signalCtx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	if err = server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err = notifications.Stop(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped gracefully")
	return nil
}
