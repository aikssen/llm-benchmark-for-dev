package domain

import (
	"context"
	"time"
)

type Channel string

const (
	Email Channel = "email"
	SMS   Channel = "sms"
	Push  Channel = "push"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusValidating Status = "validating"
	StatusQueued     Status = "queued"
	StatusSending    Status = "sending"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
)

type Notification struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient"`
	Channel   Channel   `json:"channel"`
	Content   string    `json:"content"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Retries   int       `json:"retries"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NotificationRequest struct {
	Recipients []string `json:"recipients"`
	Channel    Channel  `json:"channel"`
	Content    string   `json:"content"`
}

type NotificationRepository interface {
	Save(ctx context.Context, n *Notification) error
	GetByID(ctx context.Context, id string) (*Notification, error)
	List(ctx context.Context, limit, offset int) ([]*Notification, error)
	Update(ctx context.Context, n *Notification) error
}

type Provider interface {
	Send(ctx context.Context, n *Notification) error
	Type() Channel
}

type Validator interface {
	Validate(ctx context.Context, n *Notification) error
	Name() string
}
