// Package domain contains the core business entities of the notification
// service. It has no dependency on any other internal package (HTTP,
// storage, providers, etc.) so it can be reused and tested in isolation.
package domain

import "time"

// Channel identifies a delivery channel. New channels are added by
// introducing a new constant plus a Provider and RecipientValidator
// implementation elsewhere; nothing in this package needs to change.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// Status represents the lifecycle of a single notification (one recipient).
type Status string

const (
	// StatusPending has been accepted and validated, waiting to be delivered.
	StatusPending Status = "pending"
	// StatusProcessing is currently being delivered by a worker.
	StatusProcessing Status = "processing"
	// StatusDelivered was accepted by the provider.
	StatusDelivered Status = "delivered"
	// StatusFailed exhausted all delivery attempts or failed validation. Terminal.
	StatusFailed Status = "failed"
)

// Message carries the content of a notification. Not every field is
// meaningful for every channel (e.g. Subject is email-specific); channel
// specific validators decide what is required.
type Message struct {
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	Data    map[string]string `json:"data,omitempty"`
}

// Notification is a single, independently tracked unit of delivery work:
// one recipient, one channel, one message. A client request that targets
// multiple recipients fans out into one Notification per recipient, all
// sharing the same BatchID.
type Notification struct {
	ID        string
	BatchID   string
	Channel   Channel
	Recipient string
	Message   Message
	Status    Status
	Attempts  int
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}
