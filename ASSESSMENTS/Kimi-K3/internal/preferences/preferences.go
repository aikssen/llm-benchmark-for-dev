// Package preferences simulates an external user-preferences service that
// records per-channel opt-outs. The implementation is deterministic and
// rule-based so local runs and tests are reproducible; the Store
// interface allows swapping in a real backend later.
package preferences

import (
	"context"
	"strings"

	"notifications/internal/domain"
)

// Store answers whether a recipient has opted out of a channel.
type Store interface {
	// OptedOut reports whether recipient must not receive notifications
	// on the given channel.
	OptedOut(ctx context.Context, recipient string, channel domain.Channel) (bool, error)
}

// Simulated is a rule-based Store:
//
//   - recipients containing "optout" have opted out of every channel
//     (e.g. "optout@example.com", "+15550000optout");
//   - recipients containing "optout-<channel>" have opted out of that
//     channel only (e.g. "user+optout-sms@example.com").
//
// The rules are intentionally simple and documented so that the
// behaviour of the demo service is predictable.
type Simulated struct{}

// NewSimulated returns the rule-based preferences store.
func NewSimulated() *Simulated { return &Simulated{} }

func (Simulated) OptedOut(_ context.Context, recipient string, channel domain.Channel) (bool, error) {
	r := strings.ToLower(recipient)
	if strings.Contains(r, "optout") {
		if strings.Contains(r, "optout-"+string(channel)) {
			return true, nil
		}
		// A bare "optout" (without a channel suffix) means all channels.
		if !strings.Contains(r, "optout-") {
			return true, nil
		}
	}
	return false, nil
}
