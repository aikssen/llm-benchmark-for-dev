package domain

import (
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle state of a notification.
type Status string

const (
	StatusPending    Status = "pending"
	StatusValidating Status = "validating"
	StatusValidated  Status = "validated"
	StatusSending    Status = "sending"
	StatusSent       Status = "sent"
	StatusFailed     Status = "failed"
	StatusRejected   Status = "rejected"
)

// Notification is the aggregate root for a notification request.
type Notification struct {
	ID        string    `json:"id"`
	Channel   Channel   `json:"channel"`
	Recipients []Recipient `json:"recipients"`
	Content   Content   `json:"content"`
	Status    Status    `json:"status"`
	Result    Result    `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Recipient identifies a notification target.
type Recipient struct {
	Address string `json:"address"`
}

// Content holds the message payload.
type Content struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

// Result captures per-recipient delivery outcomes.
type Result struct {
	RecipientResults []RecipientResult `json:"recipient_results,omitempty"`
	Error            string            `json:"error,omitempty"`
}

// RecipientResult records the outcome for a single recipient.
type RecipientResult struct {
	Recipient Recipient `json:"recipient"`
	Success   bool      `json:"success"`
	Provider  string    `json:"provider,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// NewNotification creates a notification with a generated ID and initial status.
func NewNotification(channel Channel, recipients []Recipient, content Content) Notification {
	now := time.Now().UTC()
	return Notification{
		ID:         uuid.NewString(),
		Channel:    channel,
		Recipients: recipients,
		Content:    content,
		Status:     StatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// SetStatus updates the notification status and the updated-at timestamp.
func (n *Notification) SetStatus(s Status) {
	n.Status = s
	n.UpdatedAt = time.Now().UTC()
}

// MarkFailed marks the notification as failed with a reason.
func (n *Notification) MarkFailed(reason string) {
	n.SetStatus(StatusFailed)
	n.Result.Error = reason
}

// MarkRejected marks the notification as rejected with a reason.
func (n *Notification) MarkRejected(reason string) {
	n.SetStatus(StatusRejected)
	n.Result.Error = reason
}

// MarkSent records the per-recipient results and marks the notification sent.
func (n *Notification) MarkSent(results []RecipientResult) {
	n.Result.RecipientResults = results
	n.SetStatus(StatusSent)
}
