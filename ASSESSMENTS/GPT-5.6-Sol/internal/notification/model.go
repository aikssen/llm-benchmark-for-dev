package notification

import "time"

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
	default:
		return false
	}
}

type Status string

const (
	StatusQueued             Status = "queued"
	StatusProcessing         Status = "processing"
	StatusDelivered          Status = "delivered"
	StatusPartiallyDelivered Status = "partially_delivered"
	StatusFailed             Status = "failed"
	StatusRejected           Status = "rejected"
)

type DeliveryStatus string

const (
	DeliveryQueued     DeliveryStatus = "queued"
	DeliveryProcessing DeliveryStatus = "processing"
	DeliveryRetrying   DeliveryStatus = "retrying"
	DeliveryDelivered  DeliveryStatus = "delivered"
	DeliveryFailed     DeliveryStatus = "failed"
	DeliveryRejected   DeliveryStatus = "rejected"
)

func (s DeliveryStatus) Terminal() bool {
	return s == DeliveryDelivered || s == DeliveryFailed || s == DeliveryRejected
}

type Message struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

type Delivery struct {
	Recipient  string         `json:"recipient"`
	Status     DeliveryStatus `json:"status"`
	Attempts   int            `json:"attempts"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Error      string         `json:"error,omitempty"`
	ProviderID string         `json:"provider_id,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type Notification struct {
	ID         string     `json:"id"`
	Channel    Channel    `json:"channel"`
	Message    Message    `json:"message"`
	Status     Status     `json:"status"`
	Deliveries []Delivery `json:"deliveries"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func AggregateStatus(deliveries []Delivery) Status {
	if len(deliveries) == 0 {
		return StatusFailed
	}

	queued, active, delivered, rejected := 0, 0, 0, 0
	for _, delivery := range deliveries {
		switch delivery.Status {
		case DeliveryQueued:
			queued++
		case DeliveryProcessing, DeliveryRetrying:
			active++
		case DeliveryDelivered:
			delivered++
		case DeliveryRejected:
			rejected++
		}
	}

	if queued == len(deliveries) {
		return StatusQueued
	}
	if queued > 0 || active > 0 {
		return StatusProcessing
	}
	if delivered == len(deliveries) {
		return StatusDelivered
	}
	if rejected == len(deliveries) {
		return StatusRejected
	}
	if delivered > 0 {
		return StatusPartiallyDelivered
	}
	return StatusFailed
}
