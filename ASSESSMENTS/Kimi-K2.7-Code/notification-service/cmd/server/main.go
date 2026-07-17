package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"notification-service/internal/application"
	"notification-service/internal/config"
	httpinfra "notification-service/internal/infrastructure/http"
	"notification-service/internal/infrastructure/memory"
	"notification-service/internal/infrastructure/providers"
	"notification-service/pkg/workerpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	repo := memory.NewNotificationRepository()

	registry := make(providers.Registry)
	registry.Register(providers.NewEmailProvider())
	registry.Register(providers.NewSMSProvider())
	registry.Register(providers.NewPushProvider())

	pool := workerpool.NewPool(cfg.Workers, cfg.QueueSize)
	service := application.NewService(repo, registry, pool, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service.Start(ctx)

	handler := httpinfra.NewHandler(service)
	router := httpinfra.NewRouter(handler)
	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("shutdown signal received", "signal", sig.String())

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown error", "error", err)
		}
		service.Shutdown(cfg.ShutdownTimeout)
		cancel()
	}()

	logger.Info("starting server", "addr", cfg.Addr(), "workers", cfg.Workers, "queue_size", cfg.QueueSize)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
