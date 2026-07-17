package provider

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"notification-service/internal/domain"
)

var ErrTemporaryFailure = errors.New("temporary provider failure")

type SimulatedSenderConfig struct {
	Name        string
	MinLatency  time.Duration
	MaxLatency  time.Duration
	FailureRate float64
}

type SimulatedSender struct {
	name        string
	minLatency  time.Duration
	maxLatency  time.Duration
	failureRate float64
	rand        *rand.Rand
	mu          sync.Mutex
}

func NewSimulatedSender(cfg SimulatedSenderConfig) *SimulatedSender {
	if cfg.Name == "" {
		cfg.Name = "simulated-provider"
	}
	if cfg.MinLatency < 0 {
		cfg.MinLatency = 0
	}
	if cfg.MaxLatency < cfg.MinLatency {
		cfg.MaxLatency = cfg.MinLatency
	}
	if cfg.FailureRate < 0 {
		cfg.FailureRate = 0
	}
	if cfg.FailureRate > 1 {
		cfg.FailureRate = 1
	}

	return &SimulatedSender{
		name:        cfg.Name,
		minLatency:  cfg.MinLatency,
		maxLatency:  cfg.MaxLatency,
		failureRate: cfg.FailureRate,
		rand:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *SimulatedSender) Name() string {
	return s.name
}

func (s *SimulatedSender) Send(ctx context.Context, _ domain.Notification) error {
	latency, shouldFail := s.nextBehavior()
	if latency > 0 {
		timer := time.NewTimer(latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	if shouldFail {
		return ErrTemporaryFailure
	}
	return nil
}

func (s *SimulatedSender) nextBehavior() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	latency := s.minLatency
	if s.maxLatency > s.minLatency {
		delta := s.maxLatency - s.minLatency
		latency += time.Duration(s.rand.Int63n(int64(delta) + 1))
	}
	return latency, s.rand.Float64() < s.failureRate
}
