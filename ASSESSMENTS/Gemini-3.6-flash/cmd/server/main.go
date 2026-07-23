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

	"notification-service/config"
	"notification-service/internal/handler"
	"notification-service/internal/middleware"
	"notification-service/internal/provider"
	"notification-service/internal/repository"
	"notification-service/internal/service"
	"notification-service/internal/validator"
	"notification-service/pkg/resilience"
)

func main() {
	// 1. Structured Logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Load Configuration
	cfg := config.Load()
	slog.Info("Starting Notification Service", "port", cfg.Port, "workers", cfg.WorkerPoolSize)

	// 3. Repository
	repo := repository.NewMemoryRepository()

	// 4. Validation Pipeline
	pipeline := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewContentValidator(),
		validator.NewRateLimitValidator(cfg.RateLimitMaxRequests, cfg.RateLimitWindowSeconds),
		validator.NewUserPreferenceValidator([]string{"optedout@example.com", "+10000000000"}),
	)

	// 5. Provider Registry & Resilience Wiring
	registry := provider.NewRegistry()

	retryCfg := resilience.RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Factor:       2.0,
		Jitter:       true,
	}

	// Email Providers (Primary + Fallback)
	emailPrimary := provider.NewSimulatedEmailProvider("email-primary-aws-ses", 0.1, 20*time.Millisecond)
	emailFallback := provider.NewSimulatedEmailProvider("email-fallback-sendgrid", 0.0, 15*time.Millisecond)
	registry.Register(emailPrimary, retryCfg, resilience.NewCircuitBreaker(3, 1, 5*time.Second))
	registry.Register(emailFallback, retryCfg, resilience.NewCircuitBreaker(5, 1, 5*time.Second))

	// SMS Providers (Primary + Fallback)
	smsPrimary := provider.NewSimulatedSMSProvider("sms-primary-twilio", 0.1, 30*time.Millisecond)
	smsFallback := provider.NewSimulatedSMSProvider("sms-fallback-infobip", 0.0, 20*time.Millisecond)
	registry.Register(smsPrimary, retryCfg, resilience.NewCircuitBreaker(3, 1, 5*time.Second))
	registry.Register(smsFallback, retryCfg, resilience.NewCircuitBreaker(5, 1, 5*time.Second))

	// Push Providers (Primary + Fallback)
	pushPrimary := provider.NewSimulatedPushProvider("push-primary-fcm", 0.05, 15*time.Millisecond)
	pushFallback := provider.NewSimulatedPushProvider("push-fallback-apns", 0.0, 10*time.Millisecond)
	registry.Register(pushPrimary, retryCfg, resilience.NewCircuitBreaker(3, 1, 5*time.Second))
	registry.Register(pushFallback, retryCfg, resilience.NewCircuitBreaker(5, 1, 5*time.Second))

	// 6. Service Orchestrator
	orchestrator := service.NewService(repo, pipeline, registry, cfg.WorkerPoolSize, cfg.WorkerQueueCapacity)

	// 7. HTTP Routes & Middleware Wiring
	healthH := handler.NewHealthHandler()
	notifH := handler.NewNotificationHandler(orchestrator)

	mux := http.NewServeMux()
	mux.Handle("GET /health", healthH)
	mux.HandleFunc("POST /api/v1/notifications", notifH.Submit)
	mux.HandleFunc("GET /api/v1/notifications/{id}", notifH.GetStatus)
	mux.HandleFunc("GET /api/v1/notifications", notifH.List)

	handlerChain := middleware.PanicRecovery(middleware.RequestLogger(mux))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      handlerChain,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 8. Server Lifecycle & Graceful Shutdown
	serverCtx, serverStopCtx := context.WithCancel(context.Background())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		slog.Info("Shutdown signal received, initiating graceful shutdown...")

		shutdownCtx, cancel := context.WithTimeout(serverCtx, 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server forced shutdown", "error", err)
		}

		slog.Info("Draining worker pool queue...")
		orchestrator.Stop()
		slog.Info("Graceful shutdown complete")
		serverStopCtx()
	}()

	slog.Info("Server listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Server runtime failure", "error", err)
		os.Exit(1)
	}

	<-serverCtx.Done()
}
