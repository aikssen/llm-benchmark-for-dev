package domain

import "time"

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

func (c Channel) Valid() bool {
	return c == ChannelEmail || c == ChannelSMS || c == ChannelPush
}

type NotificationStatus string

const (
	StatusQueued        NotificationStatus = "queued"
	StatusProcessing    NotificationStatus = "processing"
	StatusDelivered     NotificationStatus = "delivered"
	StatusPartiallyFail NotificationStatus = "partially_failed"
	StatusFailed        NotificationStatus = "failed"
)

type DeliveryStatus string

const (
	DeliveryQueued     DeliveryStatus = "queued"
	DeliveryProcessing DeliveryStatus = "processing"
	DeliveryDelivered  DeliveryStatus = "delivered"
	DeliveryFailed     DeliveryStatus = "failed"
)

type Delivery struct {
	ID        string         `json:"id"`
	Recipient string         `json:"recipient"`
	Status    DeliveryStatus `json:"status"`
	Attempts  int            `json:"attempts"`
	LastError string         `json:"last_error,omitempty"`
}

type Notification struct {
	ID             string             `json:"id"`
	IdempotencyKey string             `json:"-"`
	Channel        Channel            `json:"channel"`
	Message        string             `json:"message"`
	Status         NotificationStatus `json:"status"`
	Deliveries     []Delivery         `json:"deliveries"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

func (n *Notification) RecalculateStatus() {
	var queued, processing, delivered, failed int
	for _, delivery := range n.Deliveries {
		switch delivery.Status {
		case DeliveryQueued:
			queued++
		case DeliveryProcessing:
			processing++
		case DeliveryDelivered:
			delivered++
		case DeliveryFailed:
			failed++
		}
	}
	switch {
	case processing > 0:
		n.Status = StatusProcessing
	case queued > 0:
		n.Status = StatusQueued
	case delivered == len(n.Deliveries):
		n.Status = StatusDelivered
	case delivered > 0 && failed > 0:
		n.Status = StatusPartiallyFail
	default:
		n.Status = StatusFailed
	}
}

func CloneNotification(source *Notification) *Notification {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Deliveries = append([]Delivery(nil), source.Deliveries...)
	return &clone
}
