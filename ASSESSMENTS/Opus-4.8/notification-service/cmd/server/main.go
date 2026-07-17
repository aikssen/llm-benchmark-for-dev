// Command server runs the notification service. It owns process lifecycle —
// configuration, logging, signal handling and graceful shutdown — and delegates
// all component wiring to the app package.
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

	"github.com/aikssen/notification-service/internal/app"
	"github.com/aikssen/notification-service/internal/config"
	"github.com/aikssen/notification-service/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	application := app.Build(cfg, log)

	// Root context is cancelled on SIGINT/SIGTERM, triggering graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application.Pool.Start(ctx)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      application.Handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", cfg.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	return shutdown(srv, application.Pool, cfg.ShutdownTimeout, log)
}

// shutdown drains the HTTP server and worker pool within the grace period.
// Order matters: stop accepting new HTTP requests first, then drain in-flight
// notifications, so nothing new enters the pipeline while it winds down.
func shutdown(srv *http.Server, pool *worker.Pool, grace time.Duration, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("http server shutdown error", slog.String("error", err.Error()))
	}
	if err := pool.Shutdown(ctx); err != nil {
		log.Error("worker pool shutdown error", slog.String("error", err.Error()))
		return err
	}
	log.Info("shutdown complete")
	return nil
}
