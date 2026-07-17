package providers

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// simulateProviderCall models an unreliable external HTTP call.
// It honors context cancellation and applies a randomized latency plus intermittent failures.
func simulateProviderCall(ctx context.Context, baseLatency time.Duration, failureRate float64) error {
	latency := baseLatency + time.Duration(rand.Int63n(int64(baseLatency)))
	select {
	case <-time.After(latency):
		if rand.Float64() < failureRate {
			return errors.New("intermittent provider error")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// withRetry retries the given operation with exponential back-off.
func withRetry(ctx context.Context, maxRetries int, op func() error) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := op(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < maxRetries {
			delay := time.Duration(1<<attempt) * 20 * time.Millisecond
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}
