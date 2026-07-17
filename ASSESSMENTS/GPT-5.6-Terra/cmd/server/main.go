package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/notification-orchestrator/internal/config"
	"github.com/example/notification-orchestrator/internal/domain"
	"github.com/example/notification-orchestrator/internal/provider"
	"github.com/example/notification-orchestrator/internal/service"
	"github.com/example/notification-orchestrator/internal/store"
	"github.com/example/notification-orchestrator/internal/transport/httpapi"
	"github.com/example/notification-orchestrator/internal/validation"
)

func main() {
	logger := log.New(os.Stdout, "notification-orchestrator ", log.LstdFlags|log.LUTC)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	repository := store.NewMemoryRepository()
	providers := provider.NewRegistry(map[domain.Channel]provider.Provider{
		domain.ChannelEmail: provider.NewLoggingProvider("email", logger),
		domain.ChannelSMS:   provider.NewLoggingProvider("sms", logger),
		domain.ChannelPush:  provider.NewLoggingProvider("push", logger),
	})
	validators := validation.NewPipeline(cfg.ValidationWorkers,
		validation.RecipientFormat{},
		validation.MessageContent{MaxBytes: cfg.MaxMessageBytes},
		validation.AllowAllPreferences{},
		validation.AllowAllRateLimit{},
	)

	dispatcher := service.New(repository, providers, validators, service.Config{
		QueueCapacity:       cfg.QueueCapacity,
		NotificationWorkers: cfg.NotificationWorkers,
		RecipientWorkers:    cfg.RecipientWorkers,
		MaxRecipients:       cfg.MaxRecipients,
		MaxAttempts:         cfg.MaxAttempts,
		AttemptTimeout:      cfg.AttemptTimeout,
		RetryBaseDelay:      cfg.RetryBaseDelay,
	}, logger)
	dispatcher.Start()

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           httpapi.NewHandler(dispatcher, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", cfg.Address)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-signals:
		logger.Printf("received %s; shutting down", sig)
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("http server failed: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("http shutdown: %v", err)
	}
	if err := dispatcher.Stop(shutdownCtx); err != nil {
		logger.Printf("dispatcher shutdown: %v", err)
	}
}
