package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"notification-service/internal/domain"
	"notification-service/pkg/resilience"
)

type ProviderConfig struct {
	Retry          resilience.RetryConfig
	CircuitBreaker *resilience.CircuitBreaker
}

type ManagedProvider struct {
	Provider Provider
	Config   ProviderConfig
}

// Registry manages providers per channel and orchestrates retries, circuit breakers, and fallbacks.
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel][]ManagedProvider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[domain.Channel][]ManagedProvider),
	}
}

// Register adds a provider to a channel's fallback pipeline.
func (r *Registry) Register(p Provider, retryCfg resilience.RetryConfig, cb *resilience.CircuitBreaker) {
	r.mu.Lock()
	defer r.mu.Unlock()

	channel := p.Channel().Normalized()
	if cb == nil {
		// Default circuit breaker: trip after 5 consecutive failures, reset after 10 seconds
		cb = resilience.NewCircuitBreaker(5, 1, 10*time.Second)
	}

	r.providers[channel] = append(r.providers[channel], ManagedProvider{
		Provider: p,
		Config: ProviderConfig{
			Retry:          retryCfg,
			CircuitBreaker: cb,
		},
	})
}

// Send attempts delivery across primary and fallback providers.
func (r *Registry) Send(ctx context.Context, item *domain.SingleRecipientItem) (string, error) {
	r.mu.RLock()
	managedList, exists := r.providers[item.Channel.Normalized()]
	r.mu.RUnlock()

	if !exists || len(managedList) == 0 {
		return "", fmt.Errorf("%w: for channel %s", domain.ErrNoProviderAvailable, item.Channel)
	}

	var lastErr error
	for _, mp := range managedList {
		p := mp.Provider
		cb := mp.Config.CircuitBreaker
		retryCfg := mp.Config.Retry

		// Execute with Circuit Breaker and Retry
		err := cb.Execute(func() error {
			return resilience.ExecuteWithRetry(ctx, retryCfg, func(execCtx context.Context) error {
				return p.Send(execCtx, item)
			})
		})

		if err == nil {
			slog.Debug("Notification delivered successfully",
				"provider_id", p.ID(),
				"recipient", item.Recipient,
				"channel", item.Channel,
			)
			return p.ID(), nil
		}

		slog.Warn("Provider delivery failed, attempting fallback if available",
			"provider_id", p.ID(),
			"recipient", item.Recipient,
			"channel", item.Channel,
			"error", err,
		)

		lastErr = err
	}

	return "", fmt.Errorf("%w: all providers failed, last error: %v", domain.ErrNoProviderAvailable, lastErr)
}
