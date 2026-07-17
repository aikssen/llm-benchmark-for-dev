// Package domain contains the core business entities of the notification
// service. It has no dependencies on infrastructure or transport layers, so
// it can be reused and tested in isolation.
package domain

import "time"

// Channel identifies a delivery channel. New channels can be added without
// modifying existing code by registering a new channel.Channel
// implementation.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// ID is a notification identifier.
type ID string

// Status is the lifecycle state of a notification or of a single recipient
// delivery attempt.
type Status string

const (
	// Overall + per-recipient states.
	StatusAccepted         Status = "accepted"
	StatusProcessing       Status = "processing"
	StatusValidationFailed Status = "validation_failed"
	StatusSending          Status = "sending"
	StatusDelivered        Status = "delivered"
	StatusFailed           Status = "failed"
	StatusPartial          Status = "partial"
)

// IsTerminal reports whether the status will not change anymore.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusDelivered, StatusFailed, StatusValidationFailed, StatusPartial:
		return true
	}
	return false
}

// Recipient is the target of a notification. Channel-specific addresses are
// optional; the relevant channel selects the address it needs.
type Recipient struct {
	ID        string `json:"id"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	PushToken string `json:"push_token,omitempty"`
}

// Message is the payload delivered to a recipient.
type Message struct {
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	Data    map[string]string `json:"data,omitempty"`
}

// RecipientResult captures the outcome of processing a single recipient.
type RecipientResult struct {
	RecipientID      string            `json:"recipient_id"`
	Status           Status            `json:"status"`
	Attempts         int               `json:"attempts"`
	LastError        string            `json:"last_error,omitempty"`
	ValidationErrors []ValidationError `json:"validation_errors,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// ValidationError describes a single validation failure with a stable code
// that clients can program against.
type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Notification is the aggregate root persisted and processed by the service.
type Notification struct {
	ID         ID                `json:"id"`
	Channel    Channel           `json:"channel"`
	Recipients []Recipient       `json:"recipients"`
	Message    Message           `json:"message"`
	Status     Status            `json:"status"`
	Results    []RecipientResult `json:"results"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// ResultFor returns the result for a recipient id, or nil if none exists yet.
func (n *Notification) ResultFor(recipientID string) *RecipientResult {
	for i := range n.Results {
		if n.Results[i].RecipientID == recipientID {
			return &n.Results[i]
		}
	}
	return nil
}

// AggregateStatus derives the overall notification status from the per
// recipient results.
func AggregateStatus(results []RecipientResult) Status {
	if len(results) == 0 {
		return StatusProcessing
	}
	delivered, failed := 0, 0
	for _, r := range results {
		switch r.Status {
		case StatusDelivered:
			delivered++
		case StatusFailed, StatusValidationFailed:
			failed++
		}
	}
	switch {
	case delivered == len(results):
		return StatusDelivered
	case failed == len(results):
		return StatusFailed
	case delivered+failed == len(results):
		return StatusPartial
	default:
		return StatusProcessing
	}
}
