package domain

import (
	"time"
)

// Channel identifies a delivery channel.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// Status represents the lifecycle of a notification delivery attempt.
type Status string

const (
	StatusPending    Status = "pending"
	StatusValidating Status = "validating"
	StatusQueued     Status = "queued"
	StatusSending    Status = "sending"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
	StatusRejected   Status = "rejected"
)

// Notification is a single delivery unit to one recipient on one channel.
type Notification struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	Recipient string    `json:"recipient"`
	Channel   Channel   `json:"channel"`
	Subject   string    `json:"subject,omitempty"`
	Body      string    `json:"body"`
	Status    Status    `json:"status"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateRequest is the API input for submitting one or more notifications.
type CreateRequest struct {
	// Recipients is required. A single request may target many recipients.
	Recipients []string `json:"recipients"`
	// Channel is required: email | sms | push
	Channel Channel `json:"channel"`
	// Subject is optional (mainly used for email).
	Subject string `json:"subject,omitempty"`
	// Body is required message content.
	Body string `json:"body"`
}

// CreateResponse is returned after accepting a notification request.
type CreateResponse struct {
	RequestID     string   `json:"request_id"`
	NotificationIDs []string `json:"notification_ids"`
	Status        Status   `json:"status"`
}

// ListFilter controls listing notifications.
type ListFilter struct {
	Status  Status
	Channel Channel
	Limit   int
	Offset  int
}

// ValidChannels returns the set of supported channels.
func ValidChannels() map[Channel]struct{} {
	return map[Channel]struct{}{
		ChannelEmail: {},
		ChannelSMS:   {},
		ChannelPush:  {},
	}
}

// IsValidChannel reports whether ch is a known channel.
func IsValidChannel(ch Channel) bool {
	_, ok := ValidChannels()[ch]
	return ok
}
