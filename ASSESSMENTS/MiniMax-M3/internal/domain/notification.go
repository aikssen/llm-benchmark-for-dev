package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

func (c Channel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return true
	}
	return false
}

func ParseChannel(s string) (Channel, error) {
	c := Channel(strings.ToLower(strings.TrimSpace(s)))
	if !c.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownChannel, s)
	}
	return c, nil
}

type Status string

const (
	StatusPending    Status = "pending"
	StatusValidating Status = "validating"
	StatusValidated  Status = "validated"
	StatusSending    Status = "sending"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
	StatusRejected   Status = "rejected"
)

func (s Status) Terminal() bool {
	switch s {
	case StatusDelivered, StatusFailed, StatusRejected:
		return true
	}
	return false
}

type Message struct {
	Subject  string            `json:"subject,omitempty"`
	Body     string            `json:"body"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Notification struct {
	ID          string     `json:"id"`
	Recipient   string     `json:"recipient"`
	Channel     Channel    `json:"channel"`
	Message     Message    `json:"message"`
	Status      Status     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

func NewNotification(id, recipient string, channel Channel, msg Message, now time.Time) *Notification {
	return &Notification{
		ID:        id,
		Recipient: recipient,
		Channel:   channel,
		Message:   msg,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (n *Notification) MarkStatus(s Status, now time.Time) {
	n.Status = s
	n.UpdatedAt = now
	if s == StatusDelivered {
		t := now
		n.DeliveredAt = &t
	}
}

func (n *Notification) RecordFailure(err error, now time.Time) {
	n.Attempts++
	n.LastError = err.Error()
	n.UpdatedAt = now
}

func (n *Notification) RecordAttemptSuccess(now time.Time) {
	n.Attempts++
	n.LastError = ""
	n.UpdatedAt = now
}

func ValidateRecipient(channel Channel, recipient string) error {
	r := strings.TrimSpace(recipient)
	if r == "" {
		return errors.New("recipient is required")
	}
	switch channel {
	case ChannelEmail:
		if !strings.Contains(r, "@") || !strings.Contains(r, ".") || strings.ContainsAny(r, " \t\n") {
			return fmt.Errorf("invalid email recipient: %q", r)
		}
	case ChannelSMS:
		if !strings.HasPrefix(r, "+") {
			return fmt.Errorf("sms recipient must be in international format (e.g. +14155551234): %q", r)
		}
		for _, c := range r[1:] {
			if c < '0' || c > '9' {
				return fmt.Errorf("sms recipient must contain only digits after '+': %q", r)
			}
		}
	case ChannelPush:
		if len(r) < 4 {
			return fmt.Errorf("push recipient (device token) too short: %q", r)
		}
	}
	return nil
}

func ValidateMessage(channel Channel, m Message) error {
	if strings.TrimSpace(m.Body) == "" {
		return errors.New("message body is required")
	}
	switch channel {
	case ChannelSMS:
		if len(m.Body) > 1600 {
			return errors.New("sms body exceeds 1600 characters")
		}
	case ChannelEmail:
		if m.Subject == "" {
			return errors.New("email subject is required")
		}
	}
	return nil
}
