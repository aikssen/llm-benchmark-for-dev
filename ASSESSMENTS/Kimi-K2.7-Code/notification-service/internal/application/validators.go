package application

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"notification-service/internal/domain"
)

// defaultValidators returns the production validation pipeline.
// Each validator is independent and runs concurrently.
func defaultValidators() []Validator {
	return []Validator{
		&recipientValidator{},
		&contentValidator{},
		&preferenceValidator{},
		&rateLimitValidator{},
	}
}

type recipientValidator struct{}

func (recipientValidator) Name() string { return "recipient" }

func (recipientValidator) Validate(_ context.Context, n domain.Notification) error {
	if len(n.Recipients) == 0 {
		return domain.ErrInvalidRecipient
	}
	for _, r := range n.Recipients {
		addr := strings.TrimSpace(r.Address)
		if addr == "" {
			return domain.ErrInvalidRecipient
		}
		if n.Channel == domain.ChannelEmail && !isValidEmail(addr) {
			return errors.New("invalid email address")
		}
		if n.Channel == domain.ChannelSMS && !isValidPhone(addr) {
			return errors.New("invalid phone number")
		}
	}
	return nil
}

type contentValidator struct{}

func (contentValidator) Name() string { return "content" }

func (contentValidator) Validate(_ context.Context, n domain.Notification) error {
	if strings.TrimSpace(n.Content.Body) == "" {
		return domain.ErrInvalidMessage
	}
	if n.Channel == domain.ChannelEmail && strings.TrimSpace(n.Content.Subject) == "" {
		return errors.New("email subject is required")
	}
	return nil
}

// preferenceValidator simulates a user preference check. In production this would query a preference store.
type preferenceValidator struct {
	mu       sync.RWMutex
	blocked  map[string]struct{}
}

func (p *preferenceValidator) Name() string { return "preference" }

func (p *preferenceValidator) Validate(_ context.Context, n domain.Notification) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.blocked == nil {
		return nil
	}
	for _, r := range n.Recipients {
		if _, ok := p.blocked[r.Address]; ok {
			return domain.ErrPreferenceBlocked
		}
	}
	return nil
}

func (p *preferenceValidator) Block(address string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blocked == nil {
		p.blocked = make(map[string]struct{})
	}
	p.blocked[address] = struct{}{}
}

// rateLimitValidator implements a simple in-memory per-recipient rate limit.
type rateLimitValidator struct {
	mu       sync.Mutex
	timestamps map[string][]int64 // keyed by channel:address
	max      int
	windowSec int64
}

func (r *rateLimitValidator) Name() string { return "rate-limit" }

func (r *rateLimitValidator) Validate(ctx context.Context, n domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.timestamps == nil {
		r.timestamps = make(map[string][]int64)
	}
	if r.max == 0 {
		r.max = 10
	}
	if r.windowSec == 0 {
		r.windowSec = 60
	}

	now := ctxTime(ctx)
	for _, rec := range n.Recipients {
		key := string(n.Channel) + ":" + rec.Address
		list := r.timestamps[key]
		// prune old entries
		cutoff := now - r.windowSec
		idx := 0
		for i, t := range list {
			if t >= cutoff {
				idx = i
				break
			}
		}
		list = list[idx:]
		if len(list) >= r.max {
			return domain.ErrRateLimitExceeded
		}
		r.timestamps[key] = append(list, now)
	}
	return nil
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func isValidEmail(email string) bool {
	return emailRegex.MatchString(email)
}

func isValidPhone(phone string) bool {
	// Minimal check: at least 7 digits, optional leading + and separators.
	digits := 0
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 7
}

// ctxTime returns the current unix time. It is overridden in tests to make the validator deterministic.
var ctxTime = func(_ context.Context) int64 {
	return timeNow().Unix()
}

// timeNow is overridden in tests.
var timeNow = time.Now
