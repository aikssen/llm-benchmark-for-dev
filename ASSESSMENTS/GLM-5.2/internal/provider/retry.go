package provider

import (
	"context"
	"math/rand/v2"
	"time"
)

// RetryOptions configures the retry middleware.
type RetryOptions struct {
	MaxAttempts int           // total attempts including the first one
	BaseDelay   time.Duration // base backoff
	MaxDelay    time.Duration // capped backoff
	Jitter      time.Duration // random jitter added per attempt
}

// WithRetry retries transient failures with exponential backoff + jitter. It
// respects ctx, so a cancelled context aborts pending retries promptly. A
// permanent error (see Permanent) is not retried.
func WithRetry(opts RetryOptions) Middleware {
	return func(next Provider) Provider {
		return &retryProvider{next: next, opts: opts}
	}
}

type retryProvider struct {
	next Provider
	opts RetryOptions
}

func (p *retryProvider) Name() string { return p.next.Name() }

func (p *retryProvider) Send(ctx context.Context, out Outbound) error {
	var lastErr error
	delay := p.opts.BaseDelay
	for attempt := 1; attempt <= p.opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := p.next.Send(ctx, out)
		if err == nil {
			return nil
		}
		if IsPermanent(err) {
			return err
		}
		lastErr = err

		if attempt == p.opts.MaxAttempts {
			break
		}
		wait := delay
		if p.opts.Jitter > 0 {
			wait += time.Duration(rand.Int64N(int64(p.opts.Jitter)))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		delay *= 2
		if delay > p.opts.MaxDelay {
			delay = p.opts.MaxDelay
		}
	}
	return lastErr
}
