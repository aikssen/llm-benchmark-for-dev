// Package domain contains the core business types of the notification
// service. It has no dependencies on infrastructure concerns (HTTP,
// storage, providers) so it can be reused from any layer.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Channel identifies a delivery channel (email, sms, push, ...).
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// Status represents the lifecycle state of a notification.
type Status string

const (
	// StatusPending: accepted by the API, waiting in the processing queue.
	StatusPending Status = "pending"
	// StatusValidating: a worker is running the validation pipeline.
	StatusValidating Status = "validating"
	// StatusRejected: at least one validation failed. Terminal.
	StatusRejected Status = "rejected"
	// StatusSending: validation passed, delivery attempts in progress.
	StatusSending Status = "sending"
	// StatusDelivered: the provider accepted the notification. Terminal.
	StatusDelivered Status = "delivered"
	// StatusFailed: delivery attempts exhausted. Terminal.
	StatusFailed Status = "failed"
)

// Terminal reports whether the status is a final state.
func (s Status) Terminal() bool {
	switch s {
	case StatusRejected, StatusDelivered, StatusFailed:
		return true
	}
	return false
}

// Valid reports whether s is a known status value.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusValidating, StatusRejected,
		StatusSending, StatusDelivered, StatusFailed:
		return true
	}
	return false
}

// Content is the message payload of a notification.
type Content struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

// Notification is the aggregate tracked by the service. A request that
// targets N recipients produces N independent Notification records that
// share the same RequestID.
type Notification struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	Channel   Channel   `json:"channel"`
	Recipient string    `json:"recipient"`
	Content   Content   `json:"content"`
	Status    Status    `json:"status"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// New creates a Notification in pending state with generated IDs.
func New(requestID string, channel Channel, recipient string, content Content, now time.Time) *Notification {
	return &Notification{
		ID:        NewID(),
		RequestID: requestID,
		Channel:   channel,
		Recipient: recipient,
		Content:   content,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Clone returns a deep copy. The store hands out clones so that callers
// can never mutate shared state without going through the store.
func (n *Notification) Clone() *Notification {
	c := *n
	return &c
}

// NewID returns a random 128-bit hex identifier.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failures are exceptional; panic is acceptable here
		// because IDs are required for the service to function.
		panic("domain: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
