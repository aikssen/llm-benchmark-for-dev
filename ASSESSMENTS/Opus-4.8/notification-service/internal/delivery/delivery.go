// Package delivery turns a single "please deliver this notification" request
// into a reliable best-effort operation against an unreliable provider.
//
// It composes three concerns that together make delivery robust:
//   - a per-attempt timeout, so a slow provider cannot block a worker forever;
//   - bounded retries with exponential backoff and jitter, for transient errors;
//   - a per-provider circuit breaker, so a provider that is down causes fast
//     local failures instead of a pile-up of doomed attempts.
//
// Providers themselves stay thin (a single Send attempt); all of this policy
// lives here so it is applied uniformly across channels.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
)

// Config tunes the delivery behaviour. Zero values fall back to sensible
// defaults in New.
type Config struct {
	Retry          RetryPolicy
	AttemptTimeout time.Duration // per-attempt deadline
	CBMaxFailures  int           // consecutive failures before a breaker opens
	CBCooldown     time.Duration // how long a breaker stays open
}

// DefaultConfig returns production-reasonable delivery settings.
func DefaultConfig() Config {
	return Config{
		Retry:          DefaultRetryPolicy(),
		AttemptTimeout: 3 * time.Second,
		CBMaxFailures:  5,
		CBCooldown:     10 * time.Second,
	}
}

// Deliverer delivers notifications via the providers in a registry, applying
// retry and circuit-breaking policy. It is safe for concurrent use.
type Deliverer struct {
	registry *provider.Registry
	cfg      Config
	log      *slog.Logger

	mu       sync.Mutex
	breakers map[domain.Channel]*CircuitBreaker
}

// New builds a Deliverer. A nil logger defaults to slog.Default.
func New(registry *provider.Registry, cfg Config, log *slog.Logger) *Deliverer {
	if cfg.AttemptTimeout <= 0 {
		cfg.AttemptTimeout = DefaultConfig().AttemptTimeout
	}
	if cfg.Retry.attempts() < 1 {
		cfg.Retry = DefaultRetryPolicy()
	}
	if log == nil {
		log = slog.Default()
	}
	return &Deliverer{
		registry: registry,
		cfg:      cfg,
		log:      log,
		breakers: make(map[domain.Channel]*CircuitBreaker),
	}
}

// Result reports the outcome of a delivery: how many attempts were made and the
// final error (nil on success).
type Result struct {
	Attempts int
	Err      error
}

// Deliver makes a best-effort attempt to deliver n, retrying transient failures
// until it succeeds, exhausts the retry budget, hits a permanent error, or the
// context is cancelled. It never blocks longer than the sum of attempt timeouts
// and backoffs, bounded by ctx.
func (d *Deliverer) Deliver(ctx context.Context, n *domain.Notification) Result {
	p, err := d.registry.Get(n.Channel)
	if err != nil {
		// No provider is a permanent, configuration-level failure.
		return Result{Attempts: 0, Err: err}
	}
	cb := d.breakerFor(n.Channel)

	var lastErr error
	maxAttempts := d.cfg.Retry.attempts()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Stop early if the caller gave up (e.g. shutdown).
		if err := ctx.Err(); err != nil {
			return Result{Attempts: attempt - 1, Err: errors.Join(errCancelled, err)}
		}

		if !cb.Allow() {
			// The provider is considered down; fail fast without an attempt.
			return Result{Attempts: attempt - 1, Err: fmt.Errorf("%w for channel %s", ErrCircuitOpen, n.Channel)}
		}

		lastErr = d.attempt(ctx, p, n)
		if lastErr == nil {
			cb.Success()
			return Result{Attempts: attempt, Err: nil}
		}
		cb.Failure()

		// A permanent error will never succeed on retry; stop immediately.
		if !provider.IsRetryable(lastErr) {
			return Result{Attempts: attempt, Err: fmt.Errorf("permanent delivery failure: %w", lastErr)}
		}

		d.log.Warn("delivery attempt failed",
			slog.String("notification_id", n.ID),
			slog.String("channel", string(n.Channel)),
			slog.Int("attempt", attempt),
			slog.String("error", lastErr.Error()),
		)

		// Back off before the next attempt, unless this was the last one.
		if attempt < maxAttempts {
			if err := sleep(ctx, d.cfg.Retry.backoff(attempt)); err != nil {
				return Result{Attempts: attempt, Err: errors.Join(errCancelled, err)}
			}
		}
	}

	return Result{Attempts: maxAttempts, Err: fmt.Errorf("delivery exhausted after %d attempts: %w", maxAttempts, lastErr)}
}

// attempt performs a single delivery bounded by the per-attempt timeout.
func (d *Deliverer) attempt(ctx context.Context, p provider.Provider, n *domain.Notification) error {
	ctx, cancel := context.WithTimeout(ctx, d.cfg.AttemptTimeout)
	defer cancel()
	return p.Send(ctx, n)
}

// breakerFor returns the circuit breaker for a channel, creating it on first use.
func (d *Deliverer) breakerFor(c domain.Channel) *CircuitBreaker {
	d.mu.Lock()
	defer d.mu.Unlock()
	cb, ok := d.breakers[c]
	if !ok {
		cb = NewCircuitBreaker(d.cfg.CBMaxFailures, d.cfg.CBCooldown)
		d.breakers[c] = cb
	}
	return cb
}

// sleep waits for d or until ctx is cancelled, whichever comes first.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var errCancelled = errors.New("delivery cancelled")
