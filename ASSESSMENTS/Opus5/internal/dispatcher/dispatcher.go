// Package dispatcher performs the provider call for a single delivery,
// wrapped in the reliability policies the service applies to every channel.
//
// Keeping retry, timeout and circuit breaking here — rather than inside each
// provider — means a new integration inherits all of it for free and cannot
// accidentally opt out of it.
package dispatcher

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/resilience"
)

// Config tunes the dispatcher.
type Config struct {
	// CallTimeout bounds a single provider call. Without it, one slow
	// provider would pin workers indefinitely and stall every channel.
	CallTimeout time.Duration
	// Retry governs re-attempts of transient failures.
	Retry resilience.RetryPolicy
	// Breaker guards each provider independently.
	Breaker resilience.BreakerConfig
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CallTimeout: 3 * time.Second,
		Retry:       resilience.DefaultRetryPolicy(),
		Breaker:     resilience.DefaultBreakerConfig(),
	}
}

// Result describes the outcome of a dispatch.
type Result struct {
	// Provider is the integration that handled (or refused) the delivery.
	Provider string
	// MessageID is the provider's identifier, set on success.
	MessageID string
	// Attempts is how many provider calls were made.
	Attempts int
}

// Dispatcher sends deliveries through the right provider.
type Dispatcher struct {
	registry *provider.Registry
	retrier  *resilience.Retrier
	breakers map[domain.Channel]*resilience.CircuitBreaker
	timeout  time.Duration
	logger   *slog.Logger
}

// Option customises a Dispatcher.
type Option func(*Dispatcher)

// WithRetrier replaces the retrier, so tests can avoid real backoff waits.
func WithRetrier(r *resilience.Retrier) Option {
	return func(d *Dispatcher) { d.retrier = r }
}

// New builds a dispatcher with one circuit breaker per registered provider.
//
// Breakers are created up front from the registry so the hot path never
// allocates one, and so a channel with no traffic still reports a state.
func New(registry *provider.Registry, cfg Config, logger *slog.Logger, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		registry: registry,
		retrier:  resilience.NewRetrier(cfg.Retry),
		breakers: make(map[domain.Channel]*resilience.CircuitBreaker),
		timeout:  cfg.CallTimeout,
		logger:   logger,
	}
	for _, p := range registry.Providers() {
		d.breakers[p.Channel()] = resilience.NewCircuitBreaker(p.Name(), cfg.Breaker)
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// BreakerState exposes a channel's breaker state, for health reporting.
func (d *Dispatcher) BreakerState(ch domain.Channel) (resilience.State, bool) {
	b, ok := d.breakers[ch]
	if !ok {
		return "", false
	}
	return b.State(), true
}

// Dispatch delivers one message, retrying transient failures.
//
// The returned error is nil on success. On failure it is the last error seen,
// which the caller records verbatim on the delivery.
func (d *Dispatcher) Dispatch(ctx context.Context, job Request) (Result, error) {
	p, err := d.registry.Get(job.Channel)
	if err != nil {
		// The channel was supported when the request was accepted but is not
		// now (a provider was removed). Permanent from this delivery's view.
		return Result{}, provider.Permanent("", provider.CodeInvalidInput,
			"no provider is registered for channel %q", job.Channel)
	}
	result := Result{Provider: p.Name()}

	breaker := d.breakers[job.Channel]

	// Counts calls that actually reached the provider, which is not the same
	// as the number of retry attempts: a call rejected by the circuit breaker
	// never leaves the process, and recording it as an attempt would tell an
	// operator the provider was tried when it was not. Written and read only
	// from inside the sequential retry loop.
	providerCalls := 0

	_, err = d.retrier.Do(ctx, func(ctx context.Context, attempt int) error {
		callCtx, cancel := context.WithTimeout(ctx, d.timeout)
		defer cancel()

		send := func(ctx context.Context) error {
			providerCalls++
			res, err := p.Send(ctx, provider.SendRequest{
				NotificationID: job.NotificationID,
				DeliveryID:     job.DeliveryID,
				Recipient:      job.Recipient,
				Message:        job.Message,
				Metadata:       job.Metadata,
				Attempt:        attempt,
			})
			if err != nil {
				return err
			}
			result.MessageID = res.MessageID
			return nil
		}

		if breaker == nil {
			return send(callCtx)
		}
		// Only transient failures count against the breaker: a provider
		// rejecting one malformed message says nothing about its health.
		return breaker.Execute(callCtx, send, resilience.IsRetryable)
	})
	result.Attempts = providerCalls

	if err != nil {
		if errors.Is(err, resilience.ErrCircuitOpen) {
			d.logger.WarnContext(ctx, "provider circuit is open, failing fast",
				slog.String("channel", job.Channel.String()),
				slog.String("provider", p.Name()),
				slog.String("delivery_id", job.DeliveryID))
			return result, provider.Permanent(p.Name(), provider.CodeUnavailable,
				"provider is unavailable (circuit breaker open)")
		}
		return result, err
	}
	return result, nil
}

// Request is the input to a dispatch. It mirrors queue.Job minus the queueing
// metadata, keeping the dispatcher independent of the transport that fed it.
type Request struct {
	NotificationID string
	DeliveryID     string
	Channel        domain.Channel
	Recipient      domain.Recipient
	Message        domain.Message
	Metadata       map[string]string
}
