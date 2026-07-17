package providers

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
)

type Provider interface {
	Name() string
	Channels() []domain.Channel
	Send(ctx context.Context, n *domain.Notification) error
}

type SimulatedProvider struct {
	providerName string
	channels     []domain.Channel

	mu              sync.Mutex
	rnd             *rand.Rand
	latencyMin      time.Duration
	latencyMax      time.Duration
	failureRate     float64
	unavailableRate float64
}

type SimulatedConfig struct {
	Name            string
	Channels        []domain.Channel
	LatencyMin      time.Duration
	LatencyMax      time.Duration
	FailureRate     float64
	UnavailableRate float64
	Seed            int64
}

func NewSimulated(cfg SimulatedConfig) *SimulatedProvider {
	if cfg.LatencyMin <= 0 {
		cfg.LatencyMin = 5 * time.Millisecond
	}
	if cfg.LatencyMax < cfg.LatencyMin {
		cfg.LatencyMax = cfg.LatencyMin
	}
	return &SimulatedProvider{
		providerName:    cfg.Name,
		channels:        cfg.Channels,
		rnd:             rand.New(rand.NewSource(cfg.Seed)),
		latencyMin:      cfg.LatencyMin,
		latencyMax:      cfg.LatencyMax,
		failureRate:     cfg.FailureRate,
		unavailableRate: cfg.UnavailableRate,
	}
}

func (s *SimulatedProvider) Name() string               { return s.providerName }
func (s *SimulatedProvider) Channels() []domain.Channel { return s.channels }

func (s *SimulatedProvider) Send(ctx context.Context, _ *domain.Notification) error {
	s.mu.Lock()
	min, max := s.latencyMin, s.latencyMax
	delay := min
	if max > min {
		delay = min + time.Duration(s.rnd.Int63n(int64(max-min)))
	}
	unavailable := s.rnd.Float64() < s.unavailableRate
	failure := s.rnd.Float64() < s.failureRate
	s.mu.Unlock()

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return ctx.Err()
	}

	switch {
	case unavailable:
		return domain.ErrProviderUnavail
	case failure:
		return errors.New("transient provider error")
	}
	return nil
}

type Registry struct {
	mu        sync.RWMutex
	providers map[domain.Channel][]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.Channel][]Provider)}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range p.Channels() {
		r.providers[c] = append(r.providers[c], p)
	}
}

func (r *Registry) Get(channel domain.Channel) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := r.providers[channel]
	if len(list) == 0 {
		return nil, false
	}
	return list[0], true
}
