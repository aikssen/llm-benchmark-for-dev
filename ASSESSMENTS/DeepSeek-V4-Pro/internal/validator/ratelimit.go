package validator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/assessment/notify-service/internal/domain"
)

type RateLimitValidator struct {
	mu       sync.Mutex
	counters map[string]int
	limit    int
	window   time.Duration
}

func NewRateLimitValidator(limit int, window time.Duration) *RateLimitValidator {
	return &RateLimitValidator{
		counters: make(map[string]int),
		limit:    limit,
		window:   window,
	}
}

func (v *RateLimitValidator) Name() string { return "rate_limit_validator" }

func (v *RateLimitValidator) Validate(_ context.Context, n *domain.Notification) (*domain.ValidationResult, error) {
	result := &domain.ValidationResult{Passed: true}

	v.mu.Lock()
	defer v.mu.Unlock()

	for _, r := range n.Recipients {
		key := fmt.Sprintf("%s:%s", r.ID, n.Channel)
		v.counters[key]++
		if v.counters[key] > v.limit {
			result.Passed = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("rate limit exceeded for recipient %s on channel %s", r.ID, n.Channel))
		}
	}

	return result, nil
}

func (v *RateLimitValidator) ResetPeriodically() {
	ticker := time.NewTicker(v.window)
	for range ticker.C {
		v.mu.Lock()
		v.counters = make(map[string]int)
		v.mu.Unlock()
	}
}
