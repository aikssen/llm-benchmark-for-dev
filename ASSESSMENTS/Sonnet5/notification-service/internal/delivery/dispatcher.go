// Package delivery drives the actual sending of a notification: it wraps
// an unreliable provider call with a per-attempt timeout, a circuit
// breaker (one per channel, so a struggling email provider doesn't affect
// SMS/push), and retries with jittered exponential backoff. A bounded
// worker pool (see pool.go) is what turns bursts of incoming work into
// predictable, capped concurrency.
package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sony/gobreaker"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
	"github.com/aikssen/notification-service/internal/store"
)

// Dispatcher delivers a single notification, handling retries and circuit
// breaking. It is safe for concurrent use by multiple workers.
type Dispatcher struct {
	registry *provider.Registry
	repo     store.Repository
	logger   *slog.Logger

	maxAttempts     int
	policy          BackoffPolicy
	providerTimeout time.Duration

	breakerFailureThreshold uint32
	breakerOpenDuration     time.Duration

	mu       sync.Mutex
	breakers map[domain.Channel]*gobreaker.CircuitBreaker
}

// Options configures a Dispatcher.
type Options struct {
	MaxAttempts             int
	Policy                  BackoffPolicy
	ProviderTimeout         time.Duration
	BreakerFailureThreshold uint32
	BreakerOpenDuration     time.Duration
}

// NewDispatcher builds a Dispatcher. registry supplies the Provider for
// each notification's channel; repo is used to persist status transitions
// as delivery progresses so GET/List reflect live state.
func NewDispatcher(registry *provider.Registry, repo store.Repository, opts Options, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		registry:                registry,
		repo:                    repo,
		logger:                  logger,
		maxAttempts:             opts.MaxAttempts,
		policy:                  opts.Policy,
		providerTimeout:         opts.ProviderTimeout,
		breakerFailureThreshold: opts.BreakerFailureThreshold,
		breakerOpenDuration:     opts.BreakerOpenDuration,
		breakers:                make(map[domain.Channel]*gobreaker.CircuitBreaker),
	}
}

// Deliver attempts to send the notification identified by id, retrying on
// failure until it succeeds, exhausts its attempt budget, or ctx is
// cancelled. It always leaves the notification in a terminal status
// (Delivered or Failed) unless ctx is cancelled mid-flight, in which case
// the notification is left for a future retry pass (see README).
func (d *Dispatcher) Deliver(ctx context.Context, id string) error {
	n, err := d.repo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("delivery: load notification %s: %w", id, err)
	}

	p, ok := d.registry.Get(n.Channel)
	if !ok {
		n.Status = domain.StatusFailed
		n.LastError = fmt.Sprintf("no provider registered for channel %q", n.Channel)
		n.UpdatedAt = time.Now()
		return d.repo.Update(ctx, n)
	}
	breaker := d.breakerFor(n.Channel)

	for attempt := 1; attempt <= d.maxAttempts; attempt++ {
		if delay := d.policy.Delay(attempt); delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		n.Attempts = attempt
		n.Status = domain.StatusProcessing
		n.UpdatedAt = time.Now()
		if err := d.repo.Update(ctx, n); err != nil {
			return fmt.Errorf("delivery: update notification %s: %w", id, err)
		}

		attemptCtx, cancel := context.WithTimeout(ctx, d.providerTimeout)
		_, sendErr := breaker.Execute(func() (any, error) {
			return nil, p.Send(attemptCtx, *n)
		})
		cancel()

		if sendErr == nil {
			n.Status = domain.StatusDelivered
			n.LastError = ""
			n.UpdatedAt = time.Now()
			return d.repo.Update(ctx, n)
		}

		n.LastError = sendErr.Error()
		d.logger.Warn("delivery attempt failed",
			"notification_id", n.ID, "channel", n.Channel, "attempt", attempt, "error", sendErr)
	}

	n.Status = domain.StatusFailed
	n.UpdatedAt = time.Now()
	if err := d.repo.Update(ctx, n); err != nil {
		return fmt.Errorf("delivery: update notification %s: %w", id, err)
	}
	return fmt.Errorf("delivery: %s exhausted %d attempts, last error: %s", id, d.maxAttempts, n.LastError)
}

// breakerFor returns the circuit breaker for channel, creating it on
// first use. One breaker per channel means an unhealthy email provider
// trips independently of SMS/push.
func (d *Dispatcher) breakerFor(channel domain.Channel) *gobreaker.CircuitBreaker {
	d.mu.Lock()
	defer d.mu.Unlock()
	if b, ok := d.breakers[channel]; ok {
		return b
	}
	logger := d.logger
	b := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:    string(channel),
		Timeout: d.breakerOpenDuration,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= d.breakerFailureThreshold
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Info("circuit breaker state change", "provider", name, "from", from, "to", to)
		},
	})
	d.breakers[channel] = b
	return b
}
