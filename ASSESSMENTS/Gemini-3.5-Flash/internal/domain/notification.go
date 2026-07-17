package domain

import (
	"errors"
	"time"
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// Validate checks if the channel is supported
func (c Channel) Validate() error {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return nil
	default:
		return errors.New("unsupported channel")
	}
}

type Status string

const (
	StatusPending          Status = "pending"
	StatusValidating       Status = "validating"
	StatusFailedValidation Status = "failed_validation"
	StatusSending          Status = "sending"
	StatusSent             Status = "sent"
	StatusFailed           Status = "failed"
)

type Notification struct {
	ID          string    `json:"id"`
	RequestID   string    `json:"request_id"`
	Recipient   string    `json:"recipient"`
	Channel     Channel   `json:"channel"`
	Content     string    `json:"content"`
	Status      Status    `json:"status"`
	ErrorReason string    `json:"error_reason,omitempty"`
	RetryCount  int       `json:"retry_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
