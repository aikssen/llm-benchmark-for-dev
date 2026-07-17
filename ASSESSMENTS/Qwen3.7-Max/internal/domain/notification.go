package domain

import (
	"errors"
	"fmt"
	"time"
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
	}
	return false
}

type Status string

const (
	StatusPending   Status = "pending"
	StatusDelivered Status = "delivered"
	StatusFailed    Status = "failed"
)

type Notification struct {
	ID        string    `json:"id"`
	Channel   Channel   `json:"channel"`
	Subject   string    `json:"subject,omitempty"`
	Message   string    `json:"message"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Recipient struct {
	Address string `json:"address"`
}

type RecipientNotification struct {
	NotificationID string `json:"notification_id"`
	Recipient      string `json:"recipient"`
	Channel        Channel `json:"channel"`
	Status         Status  `json:"status"`
	FailureReason  string  `json:"failure_reason,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

type NotificationRequest struct {
	Channel    Channel    `json:"channel"`
	Subject    string     `json:"subject,omitempty"`
	Message    string     `json:"message"`
	Recipients []string   `json:"recipients"`
}

type NotificationResponse struct {
	ID        string    `json:"id"`
	Channel   Channel   `json:"channel"`
	Subject   string    `json:"subject,omitempty"`
	Message   string    `json:"message"`
	Status    Status    `json:"status"`
	Recipients []RecipientStatus `json:"recipients"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RecipientStatus struct {
	Recipient     string     `json:"recipient"`
	Status        Status     `json:"status"`
	FailureReason string     `json:"failure_reason,omitempty"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
}

type ListFilter struct {
	Channel *Channel
	Status  *Status
	Limit   int
	Offset  int
}

var (
	ErrNotificationNotFound = errors.New("notification not found")
	ErrInvalidChannel       = errors.New("invalid notification channel")
	ErrEmptyMessage         = errors.New("message cannot be empty")
	ErrEmptyRecipients      = errors.New("recipients list cannot be empty")
	ErrInvalidRecipient     = errors.New("invalid recipient address")
	ErrRateLimitExceeded    = errors.New("rate limit exceeded")
	ErrUserOptedOut         = errors.New("user has opted out of this notification channel")
	ErrProviderUnavailable  = errors.New("notification provider is unavailable")
	ErrDeliveryFailed       = errors.New("notification delivery failed")
)

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s - %s", e.Field, e.Message)
}

type DeliveryError struct {
	Recipient string
	Channel   Channel
	Err       error
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("delivery error for %s via %s: %v", e.Recipient, e.Channel, e.Err)
}

func (e *DeliveryError) Unwrap() error {
	return e.Err
}
