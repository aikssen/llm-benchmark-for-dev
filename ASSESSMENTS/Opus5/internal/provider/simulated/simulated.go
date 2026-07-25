// Package simulated provides stand-in implementations of external delivery
// providers.
//
// They exist so the service can be run and tested end to end without third
// party credentials, and so the failure modes the service is meant to survive
// — slowness, timeouts, intermittent errors, outages — can be reproduced on
// demand. Replacing one with a real integration means implementing the same
// provider.Provider interface; no other package is affected.
package simulated

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/provider"
)

// Behaviour describes how unreliable a simulated provider should be.
//
// The two failure rates are independent slices of the [0,1) interval, so
// TransientFailureRate 0.2 and PermanentFailureRate 0.05 means 20% transient
// failures, 5% permanent ones and 75% successes.
type Behaviour struct {
	// MinLatency and MaxLatency bound the artificial round-trip time.
	MinLatency time.Duration
	MaxLatency time.Duration
	// TransientFailureRate is the probability of a retryable failure.
	TransientFailureRate float64
	// PermanentFailureRate is the probability of the provider rejecting the
	// message outright.
	PermanentFailureRate float64
}

// Option customises a provider, mainly so tests can make it deterministic.
type Option func(*Provider)

// WithRandom replaces the source of randomness. The function must return a
// value in [0,1) and must be safe for concurrent use.
func WithRandom(f func() float64) Option {
	return func(p *Provider) { p.random = f }
}

// WithSleep replaces the latency simulation.
func WithSleep(f func(ctx context.Context, d time.Duration) error) Option {
	return func(p *Provider) { p.sleep = f }
}

// Provider is a configurable, deliberately unreliable provider.
type Provider struct {
	name      string
	channel   domain.Channel
	behaviour Behaviour
	validate  func(string) error

	random func() float64
	sleep  func(ctx context.Context, d time.Duration) error

	counter atomic.Uint64
}

// Compile-time proof that the simulator honours the port.
var _ provider.Provider = (*Provider)(nil)

// New builds a simulated provider for an arbitrary channel. The dedicated
// constructors below cover the channels the service ships with.
func New(name string, channel domain.Channel, b Behaviour, validate func(string) error, opts ...Option) *Provider {
	p := &Provider{
		name:      name,
		channel:   channel,
		behaviour: b,
		validate:  validate,
		random:    lockedRandom(),
		sleep:     sleepCtx,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewEmail returns a simulated email provider.
func NewEmail(b Behaviour, opts ...Option) *Provider {
	return New("simulated-email", domain.ChannelEmail, b, ValidateEmail, opts...)
}

// NewSMS returns a simulated SMS provider.
func NewSMS(b Behaviour, opts ...Option) *Provider {
	return New("simulated-sms", domain.ChannelSMS, b, ValidatePhone, opts...)
}

// NewPush returns a simulated push notification provider.
func NewPush(b Behaviour, opts ...Option) *Provider {
	return New("simulated-push", domain.ChannelPush, b, ValidatePushToken, opts...)
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Channel() domain.Channel { return p.channel }

func (p *Provider) ValidateAddress(address string) error {
	if p.validate == nil {
		return nil
	}
	return p.validate(address)
}

// Send simulates a call to an external system: it takes time, and it may
// fail transiently, permanently, or by exceeding the caller's deadline.
func (p *Provider) Send(ctx context.Context, req provider.SendRequest) (provider.SendResult, error) {
	if err := p.sleep(ctx, p.latency()); err != nil {
		// The caller's timeout fired mid-call. The message may or may not
		// have been accepted upstream, so this is retryable and providers are
		// expected to deduplicate on their side.
		return provider.SendResult{}, &provider.Error{
			Provider: p.name,
			Code:     provider.CodeTimeout,
			Message:  "provider did not respond before the deadline",
			Retry:    true,
			Err:      err,
		}
	}

	switch roll := p.random(); {
	case roll < p.behaviour.PermanentFailureRate:
		return provider.SendResult{}, provider.Permanent(p.name, provider.CodeRejected,
			"provider rejected the message for recipient %q", req.Recipient.Address)

	case roll < p.behaviour.PermanentFailureRate+p.behaviour.TransientFailureRate:
		return provider.SendResult{}, provider.Transient(p.name, provider.CodeUnavailable,
			"provider is temporarily unavailable")
	}

	return provider.SendResult{
		MessageID: fmt.Sprintf("%s-%d", p.name, p.counter.Add(1)),
	}, nil
}

// latency picks a round-trip time inside the configured window.
func (p *Provider) latency() time.Duration {
	lo, hi := p.behaviour.MinLatency, p.behaviour.MaxLatency
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(p.random()*float64(hi-lo))
}

// sleepCtx waits for d, or returns early if the context is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
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

// lockedRandom returns a concurrency-safe source. math/rand/v2's *Rand is not
// safe for concurrent use, and the global functions cannot be seeded, so the
// provider owns a mutex-guarded generator.
func lockedRandom() func() float64 {
	var (
		mu  sync.Mutex
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	)
	return func() float64 {
		mu.Lock()
		defer mu.Unlock()
		return rng.Float64()
	}
}
