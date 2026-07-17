package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assessment/notify-service/internal/api"
	"github.com/assessment/notify-service/internal/config"
	"github.com/assessment/notify-service/internal/orchestrator"
	"github.com/assessment/notify-service/internal/provider"
	"github.com/assessment/notify-service/internal/store"
	"github.com/assessment/notify-service/internal/validator"
)

func main() {
	cfg := config.Load()

	st := store.NewMemoryStore()

	reg := provider.NewRegistry()
	reg.Register(provider.NewEmailProvider())
	reg.Register(provider.NewSMSProvider())
	reg.Register(provider.NewPushProvider())

	rlv := validator.NewRateLimitValidator(cfg.RateLimit, cfg.RateLimitWindow)
	go rlv.ResetPeriodically()

	pipe := validator.NewPipeline(
		validator.NewRecipientValidator(),
		validator.NewPreferencesValidator(),
		rlv,
		validator.NewContentValidator(),
	)

	orch := orchestrator.New(orchestrator.Config{
		MaxWorkers:    cfg.MaxWorkers,
		MaxRetries:    cfg.MaxRetries,
		RetryBackoff:  cfg.RetryBackoff,
		RequestBuffer: cfg.RequestBuffer,
	}, st, reg, pipe)

	handler := api.NewHandler(orch, st)
	router := api.NewRouter(handler)

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("server starting on port %s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}

	orch.Shutdown()
	log.Println("server stopped")
}
