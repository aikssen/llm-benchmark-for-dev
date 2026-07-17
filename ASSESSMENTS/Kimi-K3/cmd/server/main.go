// Command server runs the notification service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"notifications/internal/config"
	"notifications/internal/dispatcher"
	"notifications/internal/domain"
	"notifications/internal/httpapi"
	"notifications/internal/preferences"
	"notifications/internal/provider"
	"notifications/internal/ratelimit"
	"notifications/internal/store"
	"notifications/internal/validation"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// ---- Composition root: build the object graph -----------------------
	st := store.NewMemory()

	registry, err := provider.NewRegistry(
		provider.Wrap(provider.NewSimulated(provider.SimulatedConfig{
			Channel:    domain.ChannelEmail,
			MinLatency: cfg.SimMinLatency,
			MaxLatency: cfg.SimMaxLatency,
			ErrorRate:  cfg.SimErrorRate,
		}), resilientConfig(cfg)),
		provider.Wrap(provider.NewSimulated(provider.SimulatedConfig{
			Channel:    domain.ChannelSMS,
			MinLatency: cfg.SimMinLatency,
			MaxLatency: cfg.SimMaxLatency,
			ErrorRate:  cfg.SimErrorRate,
		}), resilientConfig(cfg)),
		provider.Wrap(provider.NewSimulated(provider.SimulatedConfig{
			Channel:    domain.ChannelPush,
			MinLatency: cfg.SimMinLatency,
			MaxLatency: cfg.SimMaxLatency,
			ErrorRate:  cfg.SimErrorRate,
		}), resilientConfig(cfg)),
	)
	if err != nil {
		return fmt.Errorf("build provider registry: %w", err)
	}

	pipeline := validation.NewPipeline(
		validation.RecipientValidator{},
		validation.ContentValidator{},
		validation.PreferencesValidator{
			Store:   preferences.NewSimulated(),
			Latency: cfg.PrefsLatency,
		},
		validation.RateLimitValidator{
			Limiter: ratelimit.New(cfg.RateLimitPerWindow, cfg.RateLimitWindow),
		},
	)

	disp := dispatcher.New(st, pipeline, registry, cfg.WorkerCount, cfg.QueueCapacity, logger)
	disp.Start()

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      httpapi.NewServer(st, disp, registry, cfg.MaxRecipients, logger).Handler(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	// ---- Run until SIGINT/SIGTERM, then shut down gracefully -------------
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr, "workers", cfg.WorkerCount, "queue_capacity", cfg.QueueCapacity)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	}

	// 1. Stop accepting HTTP requests and drain in-flight ones.
	httpCtx, httpCancel := context.WithTimeout(context.Background(), cfg.ShutdownDrain)
	defer httpCancel()
	if err := srv.Shutdown(httpCtx); err != nil {
		logger.Warn("http shutdown timed out", "error", err)
	}

	// 2. Drain the processing queue: workers finish queued notifications.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), cfg.ShutdownDrain)
	defer drainCancel()
	if err := disp.Stop(drainCtx); err != nil {
		logger.Warn("queue drain timed out; unprocessed notifications remain pending", "error", err)
	}

	logger.Info("shutdown complete")
	return nil
}

func resilientConfig(cfg config.Config) provider.ResilientConfig {
	return provider.ResilientConfig{
		AttemptTimeout:   cfg.ProviderTimeout,
		MaxAttempts:      cfg.RetryMaxAttempts,
		BaseDelay:        cfg.RetryBaseDelay,
		MaxDelay:         cfg.RetryMaxDelay,
		BreakerThreshold: cfg.BreakerThreshold,
		BreakerResetWait: cfg.BreakerResetWait,
	}
}
