package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/notifications-service/internal/api"
	"github.com/notifications-service/internal/domain"
	"github.com/notifications-service/internal/provider"
	"github.com/notifications-service/internal/service"
	"github.com/notifications-service/internal/storage"
	"github.com/notifications-service/internal/validator"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	store := storage.NewMemoryStore()

	registry := provider.NewRegistry()
	registry.Register(provider.NewSimulatedProvider(domain.ChannelEmail, logger))
	registry.Register(provider.NewSimulatedProvider(domain.ChannelSMS, logger))
	registry.Register(provider.NewSimulatedProvider(domain.ChannelPush, logger))

	chain := validator.NewChain(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
		validator.NewRateLimitValidator(100, time.Minute),
		validator.NewPreferenceValidator(),
	)

	processor := service.NewProcessor(store, registry, chain, logger, 10, 1000)
	processor.Start()
	defer processor.Stop()

	svc := service.NewNotificationService(store, registry, chain, processor, logger)

	router := api.NewRouter(svc, logger)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	logger.Info("server stopped")
}
