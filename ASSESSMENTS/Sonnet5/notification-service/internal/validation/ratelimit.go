package validation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/aikssen/notification-service/internal/domain"
)

// RateLimitValidator caps how many notifications a single recipient may
// receive per minute, regardless of channel. It runs last among
// recipient-level validators: it is the most "expensive" conceptually
// (it mutates shared state, and in a real deployment would likely be
// backed by a distributed counter such as Redis) so cheaper checks
// should reject bad input before we bother touching it.
type RateLimitValidator struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	perMin   int
}

// NewRateLimitValidator allows perMinute notifications per recipient,
// bursting up to the same amount.
func NewRateLimitValidator(perMinute int) *RateLimitValidator {
	return &RateLimitValidator{
		limiters: make(map[string]*rate.Limiter),
		perMin:   perMinute,
	}
}

func (*RateLimitValidator) Name() string { return "rate_limit" }

func (v *RateLimitValidator) Validate(_ context.Context, _ domain.Channel, recipient string) error {
	if !v.limiterFor(recipient).Allow() {
		return fmt.Errorf("%w: recipient %q exceeded rate limit of %d/minute", domain.ErrValidation, recipient, v.perMin)
	}
	return nil
}

func (v *RateLimitValidator) limiterFor(recipient string) *rate.Limiter {
	v.mu.Lock()
	defer v.mu.Unlock()
	l, ok := v.limiters[recipient]
	if !ok {
		l = rate.NewLimiter(rate.Every(time.Minute/time.Duration(v.perMin)), v.perMin)
		v.limiters[recipient] = l
	}
	return l
}
