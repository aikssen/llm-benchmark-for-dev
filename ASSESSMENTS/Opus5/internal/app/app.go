// Package app is the composition root.
//
// Every other package declares what it needs and receives it; this is the one
// place that decides which concrete implementations are used. Keeping the
// wiring in a single, dependency-free-of-globals function is what makes it
// possible to build the whole service inside a test with fakes swapped in.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/evercifuentes/notifier/internal/config"
	"github.com/evercifuentes/notifier/internal/dispatcher"
	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/httpapi"
	"github.com/evercifuentes/notifier/internal/preferences"
	"github.com/evercifuentes/notifier/internal/processing"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/provider/simulated"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/ratelimit"
	"github.com/evercifuentes/notifier/internal/resilience"
	"github.com/evercifuentes/notifier/internal/service"
	"github.com/evercifuentes/notifier/internal/store"
	"github.com/evercifuentes/notifier/internal/store/memory"
	"github.com/evercifuentes/notifier/internal/validation"
	"github.com/evercifuentes/notifier/internal/worker"
)

// Dependencies are the pieces a caller may override.
//
// They default to the production choices; tests supply fakes. This is why the
// package has no init-time globals: everything is passed in.
type Dependencies struct {
	Store       store.Store
	Preferences preferences.Store
	Registry    *provider.Registry
	Limiter     validation.Limiter
}

// App is a fully wired service instance.
type App struct {
	cfg    config.Config
	logger *slog.Logger

	Service  *service.Service
	Handler  http.Handler
	Registry *provider.Registry
	Store    store.Store

	queue      *queue.Queue
	pool       *worker.Pool
	dispatcher *dispatcher.Dispatcher
	server     *http.Server
	ready      atomic.Bool
}

// New builds the application graph. Any dependency left nil in deps gets its
// default implementation.
func New(cfg config.Config, logger *slog.Logger, deps Dependencies) *App {
	if deps.Registry == nil {
		deps.Registry = defaultRegistry(cfg)
	}
	if deps.Store == nil {
		deps.Store = memory.New()
	}
	if deps.Preferences == nil {
		deps.Preferences = preferences.NewMemoryStore(nil)
	}
	if deps.Limiter == nil {
		deps.Limiter = ratelimit.New(ratelimit.Config{
			Rate:     cfg.RateLimit.Rate,
			Interval: cfg.RateLimit.Interval,
			Burst:    cfg.RateLimit.Burst,
		})
	}

	q := queue.New(cfg.Worker.QueueCapacity)

	disp := dispatcher.New(deps.Registry, dispatcher.Config{
		CallTimeout: cfg.Provider.CallTimeout,
		Retry: resilience.RetryPolicy{
			MaxAttempts:    cfg.Retry.MaxAttempts,
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
			Multiplier:     cfg.Retry.Multiplier,
			Jitter:         cfg.Retry.Jitter,
		},
		Breaker: resilience.BreakerConfig{
			FailureThreshold: cfg.Breaker.FailureThreshold,
			OpenDuration:     cfg.Breaker.OpenDuration,
			HalfOpenMaxCalls: cfg.Breaker.HalfOpenMaxCalls,
			SuccessThreshold: cfg.Breaker.SuccessThreshold,
		},
	}, logger)

	// The validators are independent of one another, which is what lets the
	// pipeline run them concurrently and abandon the rest on first failure.
	pipeline := validation.New(
		validation.NewRecipientValidator(deps.Registry),
		validation.NewPreferenceValidator(deps.Preferences),
		validation.NewRateLimitValidator(deps.Limiter),
		validation.NewContentValidator(validation.DefaultContentLimits(), validation.ContentLimits{
			MaxBodyLength:    10_000,
			MaxSubjectLength: 255,
		}),
	)

	processor := processing.New(deps.Store, pipeline, disp, logger)
	pool := worker.New(q, processor, cfg.Worker.Count, logger)

	svc := service.New(deps.Store, q, deps.Registry,
		service.Limits{MaxRecipients: cfg.Limits.MaxRecipients}, logger)

	a := &App{
		cfg:        cfg,
		logger:     logger,
		Service:    svc,
		Registry:   deps.Registry,
		Store:      deps.Store,
		queue:      q,
		pool:       pool,
		dispatcher: disp,
	}

	a.Handler = httpapi.NewRouter(svc, a, httpapi.Config{
		MaxRequestBytes: cfg.Server.MaxRequestBytes,
		RequestTimeout:  cfg.Server.RequestTimeout,
	}, logger)

	a.server = &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      a.Handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
	return a
}

// defaultRegistry builds the simulated providers, one per supported channel.
//
// Swapping in a real integration is a change to this function alone.
func defaultRegistry(cfg config.Config) *provider.Registry {
	behaviour := simulated.Behaviour{
		MinLatency:           cfg.Provider.MinLatency,
		MaxLatency:           cfg.Provider.MaxLatency,
		TransientFailureRate: cfg.Provider.TransientFailureRate,
		PermanentFailureRate: cfg.Provider.PermanentFailureRate,
	}
	registry := provider.NewRegistry()
	registry.MustRegister(
		simulated.NewEmail(behaviour),
		simulated.NewSMS(behaviour),
		simulated.NewPush(behaviour),
	)
	return registry
}

// Run starts the workers and the HTTP server and blocks until ctx is done,
// then shuts everything down in dependency order.
func (a *App) Run(ctx context.Context) error {
	a.StartWorkers(ctx)

	serverErr := make(chan error, 1)
	go func() {
		a.logger.Info("http server listening", slog.String("addr", a.cfg.Server.Addr))
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		a.ready.Store(false)
		if err != nil {
			// Do not leave workers running behind a dead listener.
			_ = a.shutdown()
			return fmt.Errorf("http server: %w", err)
		}
		return a.shutdown()

	case <-ctx.Done():
		a.logger.Info("shutdown signal received, draining")
		return a.shutdown()
	}
}

// shutdown stops accepting requests, then drains the backlog.
//
// The order matters: closing the queue first would strand deliveries that
// belong to requests already accepted. Stop the front door, then finish the
// work already inside.
func (a *App) shutdown() error {
	a.ready.Store(false)

	// Detached from the cancelled request context: shutdown gets its own
	// budget, otherwise it would be cancelled before it starts.
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout)
	defer cancel()

	var errs []error
	if err := a.server.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("http server shutdown: %w", err))
	}
	if err := a.pool.Stop(ctx); err != nil {
		errs = append(errs, fmt.Errorf("worker pool shutdown: %w", err))
	}
	a.logger.Info("shutdown complete")
	return errors.Join(errs...)
}

// Ready implements httpapi.HealthChecker.
func (a *App) Ready() error {
	switch {
	case !a.ready.Load():
		return errors.New("service is starting up or shutting down")
	case a.queue.Closed():
		return errors.New("work queue is closed")
	case len(a.Registry.Channels()) == 0:
		return errors.New("no notification providers are registered")
	}
	return nil
}

// Details implements httpapi.HealthChecker. It reports the numbers an
// operator actually needs during an incident: how deep the backlog is, and
// which providers the service has stopped calling.
func (a *App) Details() map[string]any {
	channels := a.Registry.Channels()
	breakers := make(map[string]string, len(channels))
	for _, ch := range channels {
		if state, ok := a.dispatcher.BreakerState(ch); ok {
			breakers[ch.String()] = string(state)
		}
	}
	return map[string]any{
		"queue": map[string]any{
			"depth":    a.queue.Depth(),
			"capacity": a.queue.Capacity(),
		},
		"workers":  a.pool.Size(),
		"channels": channelNames(channels),
		"breakers": breakers,
	}
}

func channelNames(channels []domain.Channel) []string {
	names := make([]string, len(channels))
	for i, c := range channels {
		names[i] = c.String()
	}
	return names
}

// StartWorkers starts the pool without the HTTP server. Tests that drive the
// handler directly use it; Run calls it too.
func (a *App) StartWorkers(ctx context.Context) {
	a.pool.Start(ctx)
	a.ready.Store(true)
}

// StopWorkers drains the pool. It is the counterpart of StartWorkers.
func (a *App) StopWorkers(ctx context.Context) error {
	a.ready.Store(false)
	return a.pool.Stop(ctx)
}

// QueueDepth reports the current backlog.
func (a *App) QueueDepth() int { return a.queue.Depth() }

var _ httpapi.HealthChecker = (*App)(nil)
