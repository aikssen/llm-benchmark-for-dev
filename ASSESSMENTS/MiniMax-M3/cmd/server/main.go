package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/anomalyco/notification-service/internal/application"
	"github.com/anomalyco/notification-service/internal/application/validation"
	"github.com/anomalyco/notification-service/internal/config"
	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/httpapi"
	"github.com/anomalyco/notification-service/internal/infrastructure/providers"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	repo := repository.NewMemoryRepository()
	registry := providers.NewRegistry()
	if cfg.SimulatedProvider.Enabled {
		registry.Register(providers.NewSimulated(providers.SimulatedConfig{
			Name:            "sim-unified",
			Channels:        []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush},
			LatencyMin:      cfg.SimulatedProvider.LatencyMin,
			LatencyMax:      cfg.SimulatedProvider.LatencyMax,
			FailureRate:     cfg.SimulatedProvider.FailureRate,
			UnavailableRate: cfg.SimulatedProvider.UnavailableRate,
			Seed:            cfg.SimulatedProvider.Seed,
		}))
	}

	prefStore := validation.NewMemoryPreferenceStore()
	pipeline := validation.NewPipeline(
		validation.ContentValidator{},
		validation.NewPreferenceValidator(prefStore),
		validation.NewRateLimitValidator(validation.NewTokenBucket(cfg.RateLimitCapacity, cfg.RateLimitRefillPerS)),
	)

	dcfg := application.DefaultDispatcherConfig()
	dcfg.WorkerCount = cfg.WorkerCount
	dcfg.QueueSize = cfg.QueueSize
	dcfg.MaxAttempts = cfg.MaxAttempts
	dcfg.BaseBackoff = cfg.BaseBackoff
	dcfg.MaxBackoff = cfg.MaxBackoff
	dcfg.PerAttemptTimeout = cfg.PerAttemptTimeout
	dcfg.ShutdownTimeout = cfg.ShutdownTimeout

	dispatcher := application.NewDispatcher(dcfg, repo, registry, pipeline, logger)
	svc := application.NewNotificationService(repo, dispatcher, logger)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dispatcher.Start(rootCtx)

	var wg sync.WaitGroup
	if cfg.SweeperInterval > 0 && cfg.SweeperTTL > 0 {
		wg.Add(1)
		go runSweeper(rootCtx, &wg, logger, repo, cfg.SweeperInterval, cfg.SweeperTTL)
	}

	h := httpapi.NewHandler(svc)
	router := httpapi.NewRouter(h, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("http server starting", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "err", err)
			cancel()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		logger.Info("shutdown signal received")
	case <-rootCtx.Done():
		logger.Info("context cancelled")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown error", "err", err)
	}
	dispatcher.Shutdown()
	cancel()
	wg.Wait()
	logger.Info("shutdown complete")
}

func runSweeper(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, repo *repository.MemoryRepository, interval, ttl time.Duration) {
	defer wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			removed := repo.SweepOlderThan(ctx, ttl, time.Now().UTC())
			if removed > 0 {
				log.Info("sweeper removed old notifications", "count", removed)
			}
		}
	}
}
