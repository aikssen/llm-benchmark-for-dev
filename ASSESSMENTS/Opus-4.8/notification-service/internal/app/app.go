// Package app is the composition root. It assembles every concrete component
// (store, providers, validators, delivery, worker pool, HTTP handler) from a
// Config into a runnable App. Keeping wiring here — separate from main — lets
// integration tests build the exact same object graph the binary runs.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aikssen/notification-service/internal/api"
	"github.com/aikssen/notification-service/internal/config"
	"github.com/aikssen/notification-service/internal/delivery"
	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
	"github.com/aikssen/notification-service/internal/validation"
	"github.com/aikssen/notification-service/internal/worker"
)

// App bundles the assembled components and exposes the few handles the runtime
// (main) needs to serve traffic and shut down cleanly. The extra fields are
// exported so tests can drive them (e.g. opt a recipient out of a channel).
type App struct {
	Handler     http.Handler
	Pool        *worker.Pool
	Service     *service.Service
	Store       store.Store
	Preferences *validation.InMemoryPreferences
	RateLimiter validation.RateLimiter
}

// Build wires the object graph from cfg. It does not start anything; the caller
// starts the worker pool and HTTP server. A nil logger defaults to slog.Default.
func Build(cfg config.Config, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}

	// Providers: registering a channel is the only step to support it.
	registry := provider.NewRegistry()
	simCfg := provider.SimConfig{
		FailureRate:   cfg.ProviderFailureRate,
		PermanentRate: cfg.ProviderPermanentRate,
		MinLatency:    cfg.ProviderMinLatency,
		MaxLatency:    cfg.ProviderMaxLatency,
	}
	for _, ch := range []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush} {
		registry.Register(provider.NewSimulated(ch, simCfg, log))
	}

	st := store.NewMemory()

	prefs := validation.NewInMemoryPreferences()
	limiter := validation.NewFixedWindowLimiter(cfg.RateLimit, cfg.RateLimitWindow)
	pipeline := validation.NewPipeline(
		validation.RecipientValidator{},
		validation.ContentValidator{},
		validation.PreferenceValidator{Store: prefs},
		validation.RateLimitValidator{Limiter: limiter},
	)

	deliverer := delivery.New(registry, delivery.Config{
		Retry: delivery.RetryPolicy{
			MaxAttempts: cfg.MaxAttempts,
			BaseDelay:   cfg.BaseBackoff,
			MaxDelay:    cfg.MaxBackoff,
		},
		AttemptTimeout: cfg.AttemptTimeout,
		CBMaxFailures:  cfg.CBMaxFailures,
		CBCooldown:     cfg.CBCooldown,
	}, log)

	// The pool needs the service's Process method and the service needs the pool
	// as its queue: break the cycle with a late-bound closure.
	var svc *service.Service
	pool := worker.New(cfg.Workers, cfg.QueueSize, func(ctx context.Context, id string) {
		svc.Process(ctx, id)
	}, log)

	svc = service.New(service.Deps{
		Store:          st,
		Validator:      pipeline,
		Deliverer:      delivererAdapter{d: deliverer},
		Queue:          submitTimeoutQueue{pool: pool, timeout: cfg.SubmitTimeout},
		SupportedChans: registry.Channels(),
		Logger:         log,
	})

	return &App{
		Handler:     api.NewServer(svc, log).Handler(),
		Pool:        pool,
		Service:     svc,
		Store:       st,
		Preferences: prefs,
		RateLimiter: limiter,
	}
}

// delivererAdapter bridges *delivery.Deliverer to the service's deliverer
// interface, converting the result type. This small adapter is what lets the
// service package stay free of a dependency on the delivery package.
type delivererAdapter struct{ d *delivery.Deliverer }

func (a delivererAdapter) Deliver(ctx context.Context, n *domain.Notification) service.DeliveryResult {
	r := a.d.Deliver(ctx, n)
	return service.DeliveryResult{Attempts: r.Attempts, Err: r.Err}
}

// submitTimeoutQueue bounds how long Submit may block on a full worker queue, so
// a request cannot wait indefinitely under sustained overload. Beyond the
// timeout the caller gets a prompt failure the service turns into an observable
// "failed" notification.
type submitTimeoutQueue struct {
	pool    *worker.Pool
	timeout time.Duration
}

func (q submitTimeoutQueue) Submit(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, q.timeout)
	defer cancel()
	return q.pool.Submit(ctx, id)
}
