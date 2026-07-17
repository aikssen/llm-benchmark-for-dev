package provider

import (
	"context"
	"sync/atomic"
)

// AttemptRecorder counts how many times the base provider was actually called
// for a single logical send. It is request-scoped, carried through context.
type AttemptRecorder struct {
	n atomic.Int64
}

// Increment records one provider call.
func (r *AttemptRecorder) Increment() { r.n.Add(1) }

// Count returns the number of recorded attempts.
func (r *AttemptRecorder) Count() int { return int(r.n.Load()) }

type attemptKey struct{}

// WithAttemptRecorder attaches a recorder to the context so innermost
// middleware can count attempts for the current logical send.
func WithAttemptRecorder(ctx context.Context, r *AttemptRecorder) context.Context {
	return context.WithValue(ctx, attemptKey{}, r)
}

// AttemptRecorderFrom returns the recorder from the context, or nil.
func AttemptRecorderFrom(ctx context.Context) *AttemptRecorder {
	r, _ := ctx.Value(attemptKey{}).(*AttemptRecorder)
	return r
}

// WithAttempts wraps a provider so each actual base call is counted in the
// recorder found in the context. Place it as the innermost middleware.
func WithAttempts() Middleware {
	return func(next Provider) Provider {
		return &attemptsProvider{next: next}
	}
}

type attemptsProvider struct{ next Provider }

func (p *attemptsProvider) Name() string { return p.next.Name() }

func (p *attemptsProvider) Send(ctx context.Context, out Outbound) error {
	if r := AttemptRecorderFrom(ctx); r != nil {
		r.Increment()
	}
	return p.next.Send(ctx, out)
}
