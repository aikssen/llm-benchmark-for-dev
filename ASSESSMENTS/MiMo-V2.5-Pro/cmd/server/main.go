package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/notification-service/internal/config"
	"github.com/notification-service/internal/handler"
	"github.com/notification-service/internal/provider"
	"github.com/notification-service/internal/repository"
	"github.com/notification-service/internal/service"
	"github.com/notification-service/internal/validator"
	"github.com/notification-service/pkg/logger"
)

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)

	log.Info("starting notification service",
		"port", cfg.Server.Port,
		"workers", cfg.Worker.Count,
	)

	repo := repository.NewMemoryRepository()

	providerRegistry := provider.NewRegistry()
	providerRegistry.Register(provider.NewEmailProvider(log))
	providerRegistry.Register(provider.NewSMSProvider(log))
	providerRegistry.Register(provider.NewPushProvider(log))

	pipeline := validator.NewPipeline(log,
		validator.NewChannelValidator(),
		validator.NewRecipientValidator(),
		validator.NewContentValidator(10000),
		validator.NewRateLimiterValidator(100, time.Minute),
	)

	notifService := service.NewNotificationService(
		repo,
		providerRegistry,
		pipeline,
		log,
		cfg.Worker.Count,
		cfg.Worker.QueueSize,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	notifService.Start(ctx)

	router := mux.NewRouter()
	h := handler.NewHandler(notifService, log)
	h.RegisterRoutes(router)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		log.Info("HTTP server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	log.Info("received shutdown signal", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("HTTP server shutdown error", "error", err)
	}

	cancel()
	notifService.Stop()

	log.Info("notification service stopped")
}
