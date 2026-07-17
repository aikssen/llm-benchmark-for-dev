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

	"github.com/evercifuentes/notifications/internal/api"
	"github.com/evercifuentes/notifications/internal/config"
	"github.com/evercifuentes/notifications/internal/domain"
	"github.com/evercifuentes/notifications/internal/provider"
	"github.com/evercifuentes/notifications/internal/service"
	"github.com/evercifuentes/notifications/internal/store"
	"github.com/evercifuentes/notifications/internal/validation"
	"github.com/evercifuentes/notifications/internal/worker"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	st := store.NewMemoryStore()
	prefs := validation.NewMemoryPreferences()
	limiter := validation.NewSlidingWindowLimiter(cfg.RateLimitPerMinute, time.Minute)

	// Validators are independent; pipeline short-circuits on first failure.
	pipeline := validation.NewPipeline(
		validation.NewRecipientValidator(),
		validation.NewContentValidator(2000),
		validation.NewPreferencesValidator(prefs),
		validation.NewRateLimitValidator(limiter),
	)

	// Providers are registered by channel; new channels require only a new registration.
	registry := provider.NewRegistry()
	for _, ch := range []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush} {
		registry.Register(provider.NewSimulatedProvider(
			ch,
			cfg.ProviderFailRate,
			cfg.ProviderMinLatency,
			cfg.ProviderMaxLatency,
		))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool := worker.NewPool(st, pipeline, registry, worker.Config{
		Workers:    cfg.WorkerCount,
		QueueSize:  cfg.QueueSize,
		MaxRetries: cfg.MaxRetries,
		BaseDelay:  cfg.RetryBaseDelay,
		Logger:     log,
	})
	pool.Start(ctx)

	svc := service.New(st, pool)
	handler := api.NewHandler(svc)
	mux := http.NewServeMux()
	handler.Routes(mux)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           loggingMiddleware(log, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("server listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	pool.Stop()
	log.Info("shutdown complete")
}

func loggingMiddleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
