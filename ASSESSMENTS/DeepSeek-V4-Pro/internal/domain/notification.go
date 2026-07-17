package domain

import (
	"errors"
	"time"
)

type Status string

const (
	StatusPending       Status = "pending"
	StatusValidating    Status = "validating"
	StatusSending       Status = "sending"
	StatusSent          Status = "sent"
	StatusPartialSent   Status = "partial_sent"
	StatusFailed        Status = "failed"
	StatusValidationErr Status = "validation_error"
)

var (
	ErrEmptyRecipients  = errors.New("at least one recipient is required")
	ErrEmptyMessage     = errors.New("message body is required")
	ErrInvalidChannel   = errors.New("invalid channel")
	ErrNotFound         = errors.New("notification not found")
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

func (c Channel) IsValid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return true
	default:
		return false
	}
}

type Recipient struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
}

type Message struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

type DeliveryResult struct {
	RecipientID string `json:"recipient_id"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	SentAt      time.Time `json:"sent_at,omitempty"`
}

type Notification struct {
	ID              string           `json:"id"`
	Channel         Channel          `json:"channel"`
	Recipients      []Recipient      `json:"recipients"`
	Message         Message          `json:"message"`
	Status          Status           `json:"status"`
	ValidationErrors []string        `json:"validation_errors,omitempty"`
	DeliveryResults []DeliveryResult `json:"delivery_results,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type NewNotification struct {
	Channel    Channel     `json:"channel"`
	Recipients []Recipient `json:"recipients"`
	Message    Message     `json:"message"`
}

func (n *NewNotification) Validate() error {
	if len(n.Recipients) == 0 {
		return ErrEmptyRecipients
	}
	if n.Message.Body == "" {
		return ErrEmptyMessage
	}
	if !n.Channel.IsValid() {
		return ErrInvalidChannel
	}
	return nil
}
