package domain

import (
	"time"
)

// Channel represents the delivery channel for a notification.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// IsValid checks if the channel is a valid known channel.
func (c Channel) IsValid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return true
	default:
		return false
	}
}

// Status represents the current state of a notification.
type Status string

const (
	StatusPending    Status = "pending"
	StatusValidating Status = "validating"
	StatusQueued     Status = "queued"
	StatusSending    Status = "sending"
	StatusSent       Status = "sent"
	StatusFailed     Status = "failed"
	StatusPartial    Status = "partial" // For multi-recipient: some succeeded, some failed
)

// Notification represents a notification request in the system.
type Notification struct {
	ID         string       `json:"id"`
	Recipients []Recipient  `json:"recipients"`
	Channel    Channel      `json:"channel"`
	Content    Content      `json:"content"`
	Status     Status       `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Results    []Result     `json:"results,omitempty"`
	Metadata   Metadata     `json:"metadata,omitempty"`
}

// Recipient represents the target of a notification.
type Recipient struct {
	ID    string `json:"id,omitempty"`
	Value string `json:"value"` // email address, phone number, or device token
}

// Content represents the message content of a notification.
type Content struct {
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	HTML    string            `json:"html,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
}

// Result represents the delivery result for a single recipient.
type Result struct {
	Recipient Recipient `json:"recipient"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
}

// Metadata holds additional information about a notification.
type Metadata struct {
	Priority   string `json:"priority,omitempty"`
	CallbackURL string `json:"callback_url,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

// CreateNotificationRequest represents the API request to create a notification.
type CreateNotificationRequest struct {
	Recipients []Recipient `json:"recipients" validate:"required,min=1"`
	Channel    Channel     `json:"channel" validate:"required"`
	Content    Content     `json:"content" validate:"required"`
	Metadata   Metadata    `json:"metadata,omitempty"`
}

// ListNotificationsParams contains parameters for listing notifications.
type ListNotificationsParams struct {
	Channel Channel
	Status  Status
	Limit   int
	Offset  int
}
