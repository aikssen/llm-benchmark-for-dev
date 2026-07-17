package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/evercifuentes/notifications/internal/domain"
)

// ErrTemporary indicates a retryable provider failure.
var ErrTemporary = errors.New("temporary provider failure")

// Provider delivers a notification on a specific channel.
// New channels are added by implementing this interface and registering them.
type Provider interface {
	// Channel returns the channel this provider handles.
	Channel() domain.Channel
	// Send attempts delivery. Temporary failures should return ErrTemporary
	// (possibly wrapped) so the worker can retry.
	Send(ctx context.Context, n *domain.Notification) error
}

// Registry maps channels to providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.Channel]Provider)}
}

// Register adds a provider. Later registrations for the same channel replace the previous one.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Channel()] = p
}

// Get returns the provider for a channel.
func (r *Registry) Get(ch domain.Channel) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[ch]
	if !ok {
		return nil, fmt.Errorf("no provider registered for channel %q", ch)
	}
	return p, nil
}

// SimulatedProvider is an unreliable external provider used for local development.
type SimulatedProvider struct {
	channel    domain.Channel
	failRate   float64
	minLatency time.Duration
	maxLatency time.Duration
	rng        *rand.Rand
	mu         sync.Mutex
}

// NewSimulatedProvider creates a simulated provider for the given channel.
func NewSimulatedProvider(ch domain.Channel, failRate float64, minLatency, maxLatency time.Duration) *SimulatedProvider {
	if maxLatency < minLatency {
		maxLatency = minLatency
	}
	return &SimulatedProvider{
		channel:    ch,
		failRate:   failRate,
		minLatency: minLatency,
		maxLatency: maxLatency,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Channel implements Provider.
func (p *SimulatedProvider) Channel() domain.Channel {
	return p.channel
}

// Send simulates latency and intermittent failures.
func (p *SimulatedProvider) Send(ctx context.Context, n *domain.Notification) error {
	latency := p.randomLatency()
	timer := time.NewTimer(latency)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}

	p.mu.Lock()
	fail := p.rng.Float64() < p.failRate
	p.mu.Unlock()

	if fail {
		return fmt.Errorf("%w: %s provider unavailable for recipient %s", ErrTemporary, p.channel, n.Recipient)
	}
	return nil
}

func (p *SimulatedProvider) randomLatency() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.maxLatency == p.minLatency {
		return p.minLatency
	}
	delta := p.maxLatency - p.minLatency
	return p.minLatency + time.Duration(p.rng.Int63n(int64(delta)+1))
}
