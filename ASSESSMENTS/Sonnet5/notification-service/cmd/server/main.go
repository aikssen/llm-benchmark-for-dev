// Command server runs the notification orchestration HTTP API.
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

	"github.com/aikssen/notification-service/internal/api"
	"github.com/aikssen/notification-service/internal/config"
	"github.com/aikssen/notification-service/internal/delivery"
	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
	"github.com/aikssen/notification-service/internal/validation"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	repo := store.NewMemoryRepository()

	// --- Validation pipeline ---
	// Order is deliberate: cheapest/local checks first so a failure short
	// circuits before any more expensive work happens (see
	// internal/validation package docs).
	prefStore := validation.NewInMemoryPreferenceStore()
	pipeline := validation.NewPipeline(
		[]validation.RequestValidator{
			validation.ContentValidator{},
		},
		[]validation.RecipientValidator{
			validation.FormatValidator{},
			validation.PreferenceValidator{Store: prefStore},
			validation.NewRateLimitValidator(cfg.RateLimitPerMinute),
		},
	)

	// --- Providers ---
	// Adding a new channel later means adding a Provider implementation
	// and registering it here; nothing else in the codebase changes.
	registry := provider.NewRegistry(
		provider.NewSimulated(domain.ChannelEmail, cfg.ProviderFailureRate, cfg.ProviderMinLatency, cfg.ProviderMaxLatency),
		provider.NewSimulated(domain.ChannelSMS, cfg.ProviderFailureRate, cfg.ProviderMinLatency, cfg.ProviderMaxLatency),
		provider.NewSimulated(domain.ChannelPush, cfg.ProviderFailureRate, cfg.ProviderMinLatency, cfg.ProviderMaxLatency),
	)

	// --- Delivery: bounded worker pool + retrying/circuit-broken dispatcher ---
	dispatcher := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts:             cfg.MaxDeliveryAttempts,
		Policy:                  delivery.BackoffPolicy{Base: cfg.BaseBackoff, Max: cfg.MaxBackoff},
		ProviderTimeout:         cfg.ProviderTimeout,
		BreakerFailureThreshold: cfg.BreakerFailureThreshold,
		BreakerOpenDuration:     cfg.BreakerOpenDuration,
	}, logger)

	pool := delivery.NewWorkerPool(cfg.WorkerPoolSize, cfg.QueueCapacity, dispatcher, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool.Start(ctx)

	svc := service.New(repo, pipeline, pool, logger)
	handler := api.NewHandler(svc, logger)
	router := api.NewRouter(handler)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}
	if err := pool.Shutdown(shutdownCtx); err != nil {
		logger.Error("worker pool shutdown error", "error", err)
	}
	logger.Info("shutdown complete")
}
