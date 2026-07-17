// Package domain defines the core business entities and value objects of the
// notification service. It has no dependencies on transport, storage or
// third-party providers so that the domain model can evolve independently of
// the infrastructure around it.
package domain

import "time"

// Channel identifies the delivery medium of a notification. New channels can be
// added here and wired up with a matching provider without touching the rest of
// the domain.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// Valid reports whether the channel is one the service knows about. Validation
// of whether a *provider* exists for the channel happens separately, in the
// provider registry.
func (c Channel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return true
	default:
		return false
	}
}

// Status represents the lifecycle state of a single notification. The zero
// value is intentionally the empty string so an uninitialised notification is
// never mistaken for a valid state.
type Status string

const (
	// StatusPending: accepted and queued, not yet picked up by a worker.
	StatusPending Status = "pending"
	// StatusValidating: a worker is running the validation pipeline.
	StatusValidating Status = "validating"
	// StatusDelivering: validations passed, delivery attempts are in progress.
	StatusDelivering Status = "delivering"
	// StatusDelivered: a provider accepted the notification. Terminal.
	StatusDelivered Status = "delivered"
	// StatusFailed: validation failed or delivery was exhausted. Terminal.
	StatusFailed Status = "failed"
)

// Terminal reports whether the status is a final state that will not change.
func (s Status) Terminal() bool {
	return s == StatusDelivered || s == StatusFailed
}

// Message holds the channel-agnostic content of a notification. Not every field
// is meaningful for every channel (e.g. SMS ignores Subject); channel-specific
// validation lives in the validation pipeline.
type Message struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

// Notification is the unit of work tracked by the system. A client request that
// targets multiple recipients is expanded into one Notification per recipient
// so that each has an independent status and delivery lifecycle.
type Notification struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	Channel   Channel   `json:"channel"`
	Recipient string    `json:"recipient"`
	Message   Message   `json:"message"`
	Status    Status    `json:"status"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
