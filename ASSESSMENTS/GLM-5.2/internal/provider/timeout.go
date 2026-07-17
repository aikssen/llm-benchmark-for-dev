package provider

import (
	"context"
	"time"
)

// WithTimeout bounds the maximum time a single Send call may take.
func WithTimeout(d time.Duration) Middleware {
	return func(next Provider) Provider {
		return &timeoutProvider{next: next, timeout: d}
	}
}

type timeoutProvider struct {
	next    Provider
	timeout time.Duration
}

func (p *timeoutProvider) Name() string { return p.next.Name() }

func (p *timeoutProvider) Send(ctx context.Context, out Outbound) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.next.Send(ctx, out)
}
