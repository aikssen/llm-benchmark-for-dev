package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"notification-service/internal/domain"
)

var (
	ErrNoProvider = errors.New("no provider registered for channel")
)

// Provider defines the interface for delivering notifications to external systems
type Provider interface {
	Name() string
	Channel() domain.Channel
	Send(ctx context.Context, n *domain.Notification) error
}

// MockProvider is a generic simulated provider with configurable latency and failure rates
type MockProvider struct {
	name        string
	channel     domain.Channel
	failRate    float64 // probability of failure (0.0 to 1.0)
	baseLatency time.Duration
	rng         *rand.Rand
	rngMu       sync.Mutex
}

// NewMockProvider creates a new simulated provider
func NewMockProvider(name string, channel domain.Channel, failRate float64, baseLatency time.Duration) *MockProvider {
	return &MockProvider{
		name:        name,
		channel:     channel,
		failRate:    failRate,
		baseLatency: baseLatency,
		// Seed local rand for concurrency safety
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *MockProvider) Name() string {
	return p.name
}

func (p *MockProvider) Channel() domain.Channel {
	return p.channel
}

func (p *MockProvider) Send(ctx context.Context, n *domain.Notification) error {
	// Simulate provider latency
	select {
	case <-time.After(p.baseLatency):
	case <-ctx.Done():
		return ctx.Err()
	}

	p.rngMu.Lock()
	randomVal := p.rng.Float64()
	p.rngMu.Unlock()

	// Simulate random failure
	if randomVal < p.failRate {
		return fmt.Errorf("provider %s failed to deliver: temporary service interruption", p.name)
	}

	return nil
}

// Registry manages registered providers for each channel
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel][]Provider
}

// NewRegistry initializes an empty provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[domain.Channel][]Provider),
	}
}

// Register adds a provider to the registry
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Channel()] = append(r.providers[p.Channel()], p)
}

// GetProviders retrieves the registered providers for a given channel
func (r *Registry) GetProviders(channel domain.Channel) ([]Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provs, exists := r.providers[channel]
	if !exists || len(provs) == 0 {
		return nil, ErrNoProvider
	}

	// Return a copy to prevent external mutation
	copied := make([]Provider, len(provs))
	copy(copied, provs)
	return copied, nil
}
