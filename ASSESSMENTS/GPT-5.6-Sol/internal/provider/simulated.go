package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"notification/internal/notification"
)

type Registry map[notification.Channel]notification.Provider

func (r Registry) Provider(channel notification.Channel) (notification.Provider, bool) {
	value, ok := r[channel]
	return value, ok
}

type Simulated struct {
	name        string
	latency     time.Duration
	failureRate float64
	randomMu    sync.Mutex
	random      *rand.Rand
	sequence    atomic.Uint64
}

func NewSimulated(name string, latency time.Duration, failureRate float64, seed int64) *Simulated {
	return &Simulated{
		name:        name,
		latency:     latency,
		failureRate: failureRate,
		random:      rand.New(rand.NewSource(seed)),
	}
}

func (p *Simulated) Send(ctx context.Context, _ notification.ProviderMessage) (notification.ProviderResult, error) {
	timer := time.NewTimer(p.latency)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return notification.ProviderResult{}, notification.ProviderError{
			Err:         ctx.Err(),
			CanRetry:    errors.Is(ctx.Err(), context.DeadlineExceeded),
			Description: "provider request timed out",
		}
	}

	p.randomMu.Lock()
	failed := p.random.Float64() < p.failureRate
	p.randomMu.Unlock()
	if failed {
		return notification.ProviderResult{}, notification.ProviderError{
			Err:         errors.New("simulated transient failure"),
			CanRetry:    true,
			Description: "provider temporarily unavailable",
		}
	}

	return notification.ProviderResult{ID: fmt.Sprintf("%s-%d", p.name, p.sequence.Add(1))}, nil
}
