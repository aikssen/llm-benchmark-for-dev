package resilience

import (
	"context"
	"math/rand"
	"time"
)

type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Factor       float64
	Jitter       bool
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Factor:       2.0,
		Jitter:       true,
	}
}

// ExecuteWithRetry runs operation with exponential backoff and jitter.
func ExecuteWithRetry(ctx context.Context, config RetryConfig, fn func(ctx context.Context) error) error {
	var err error
	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err = fn(ctx)
		if err == nil {
			return nil
		}

		if attempt == config.MaxAttempts {
			break
		}

		currentDelay := delay
		if config.Jitter {
			// Add up to 50% random jitter
			jitterRange := float64(currentDelay) * 0.5
			jitter := time.Duration((rand.Float64()*2 - 1) * jitterRange)
			currentDelay += jitter
			if currentDelay < 0 {
				currentDelay = config.InitialDelay
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(currentDelay):
		}

		delay = time.Duration(float64(delay) * config.Factor)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return err
}
