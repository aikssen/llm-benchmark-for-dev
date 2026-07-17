package notification

import (
	"context"
	"errors"
)

var (
	ErrNotFound            = errors.New("notification not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
)

type Page struct {
	Items  []Notification
	Total  int
	Limit  int
	Offset int
}

type Repository interface {
	Create(ctx context.Context, value Notification, idempotencyKey, fingerprint string) (Notification, bool, error)
	Get(ctx context.Context, id string) (Notification, error)
	GetByIdempotencyKey(ctx context.Context, key, fingerprint string) (Notification, error)
	SaveDelivery(ctx context.Context, notificationID string, index int, delivery Delivery) (Notification, error)
	List(ctx context.Context, limit, offset int) (Page, error)
}

type ProviderMessage struct {
	NotificationID string
	Recipient      string
	Message        Message
}

type ProviderResult struct {
	ID string
}

type Provider interface {
	Send(ctx context.Context, message ProviderMessage) (ProviderResult, error)
}

type ProviderRegistry interface {
	Provider(channel Channel) (Provider, bool)
}

type Validator interface {
	Name() string
	Validate(ctx context.Context, channel Channel, recipient string, message Message) error
}

type CodedError interface {
	error
	Code() string
}

type RetryableError interface {
	error
	Retryable() bool
}
