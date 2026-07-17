// Command notification-service starts the notification orchestration API.
//
// It wires the domain, validation, provider, channel, storage, processing and
// transport layers, starts the worker pool and HTTP server, and performs a
// graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/notification-service/internal/channel"
	"github.com/example/notification-service/internal/config"
	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/observability"
	"github.com/example/notification-service/internal/preference"
	"github.com/example/notification-service/internal/processing"
	"github.com/example/notification-service/internal/provider"
	"github.com/example/notification-service/internal/service"
	"github.com/example/notification-service/internal/storage"
	"github.com/example/notification-service/internal/transport/http"
	"github.com/example/notification-service/internal/validation"
)

func main() {
	cfg := config.FromEnv()
	log := observability.NewLogger(cfg.LogLevel)

	supported := []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush}

	// Infrastructure.
	repo := storage.NewMemory()
	prefStore := preference.NewMemoryStore(true) // opt-in by default; see README.
	rateLimiter := validation.NewSlidingWindowLimiter(cfg.RateLimitWindow, cfg.RateLimitMax)

	// Validation pipeline (independent validators, fail-fast).
	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(),
		validation.NewPreferenceValidator(prefStore),
		validation.NewRateLimitValidator(rateLimiter),
	)

	// Providers wrapped with middleware: circuit breaker around retry around
	// timeout around the attempt-counting innermost wrapper around the
	// simulated base provider.
	buildProvider := func(name string) provider.Provider {
		base := provider.NewSimulator(provider.SimulatorConfig{
			Name:        name,
			FailureProb: cfg.ProviderFailureProb,
			SlowProb:    cfg.ProviderSlowProb,
			SlowDelay:   cfg.ProviderSlowDelay,
			MinDelay:    cfg.ProviderMinDelay,
		})
		return provider.WithCircuitBreaker(provider.CircuitBreakerOptions{
			FailureThreshold: cfg.CircuitFailures,
			Cooldown:         cfg.CircuitCooldown,
		})(
			provider.WithRetry(provider.RetryOptions{
				MaxAttempts: cfg.RetryAttempts,
				BaseDelay:   cfg.RetryBase,
				MaxDelay:    cfg.RetryMax,
			})(
				provider.WithTimeout(cfg.SendTimeout)(
					provider.WithAttempts()(base),
				),
			),
		)
	}

	channels := channel.NewRegistry().
		Register(channel.NewEmail(buildProvider("email-simulator"))).
		Register(channel.NewSMS(buildProvider("sms-simulator"))).
		Register(channel.NewPush(buildProvider("push-simulator")))

	// Processing.
	queue := processing.NewQueue(cfg.QueueCapacity)
	processor := processing.New(
		processing.Config{
			Workers:              cfg.Workers,
			RecipientConcurrency: cfg.RecipientConcurrency,
		},
		queue, repo, channels, pipeline, log,
	)

	// Service + transport.
	svc := service.New(repo, queue, supported)
	router := httptransport.NewRouter(svc, log, cfg.ListDefaultLimit, cfg.ListMaxLimit)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Lifecycle.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	processor.Start(rootCtx)
	log.Info("notification service starting",
		"addr", cfg.HTTPAddr,
		"workers", cfg.Workers,
		"queue_capacity", cfg.QueueCapacity,
		"channels", supported,
	)

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
			stop()
		}
	}()

	<-rootCtx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "err", err)
	}
	processor.Stop()
	log.Info("notification service stopped")
}
