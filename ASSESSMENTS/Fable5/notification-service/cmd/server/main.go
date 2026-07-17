// Command server runs the notification orchestration service.
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
	"time"

	"notificationservice/internal/api"
	"notificationservice/internal/config"
	"notificationservice/internal/domain"
	"notificationservice/internal/provider"
	"notificationservice/internal/store"
	"notificationservice/internal/validation"
	"notificationservice/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// --- Wiring -----------------------------------------------------------

	st := store.NewMemory()

	registry := provider.NewRegistry()
	simCfg := provider.SimulatedConfig{
		MinLatency:           cfg.ProviderMinLatency,
		MaxLatency:           cfg.ProviderMaxLatency,
		TransientFailureRate: cfg.ProviderTransientFailureRate,
		PermanentFailureRate: cfg.ProviderPermanentFailureRate,
	}
	for _, ch := range domain.Channels() {
		if err := registry.Register(provider.NewSimulated(ch, simCfg, logger)); err != nil {
			return fmt.Errorf("registering provider: %w", err)
		}
	}

	// 10ms simulates the latency of a remote preference service lookup.
	prefs := validation.NewStaticPreferences(10*time.Millisecond, cfg.OptedOutRecipients...)
	pipeline := validation.NewPipeline(
		validation.Recipient{},
		validation.Content{},
		validation.Preferences{Checker: prefs},
		validation.NewRateLimit(cfg.RateLimit, cfg.RateLimitWindow),
	)

	pool := worker.New(worker.Config{
		Workers:           cfg.Workers,
		QueueCapacity:     cfg.QueueCapacity,
		MaxAttempts:       cfg.MaxAttempts,
		RetryBaseDelay:    cfg.RetryBaseDelay,
		RetryMaxDelay:     cfg.RetryMaxDelay,
		ValidationTimeout: cfg.ValidationTimeout,
		SendTimeout:       cfg.SendTimeout,
	}, st, registry, pipeline, logger)

	handler := api.New(api.Config{
		MaxRecipients:   cfg.MaxRecipients,
		MaxBodyBytes:    cfg.MaxBodyBytes,
		DefaultPageSize: cfg.DefaultPageSize,
		MaxPageSize:     cfg.MaxPageSize,
	}, st, pool, logger)

	// --- Run --------------------------------------------------------------

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	pool.Start(workerCtx)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.Addr, "workers", cfg.Workers, "queue_capacity", cfg.QueueCapacity)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting requests first, then stop workers.
	// Workers finish their in-flight delivery; jobs still queued are dropped
	// (acceptable with an in-memory store; see README for the durable path).
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown did not complete cleanly", "error", err)
	}
	cancelWorkers()
	pool.Wait()
	logger.Info("shutdown complete")
	return nil
}
