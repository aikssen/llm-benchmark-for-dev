package domain

import "time"

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusValidating Status = "validating"
	StatusSending    Status = "sending"
	StatusDelivered  Status = "delivered"
	StatusRejected   Status = "rejected"
	StatusFailed     Status = "failed"
)

type Message struct {
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	Data    map[string]string `json:"data,omitempty"`
}

type Notification struct {
	ID              string     `json:"id"`
	RequestID       string     `json:"request_id"`
	Recipient       string     `json:"recipient"`
	Channel         Channel    `json:"channel"`
	Message         Message    `json:"message"`
	Status          Status     `json:"status"`
	Attempts        int        `json:"attempts"`
	Provider        string     `json:"provider,omitempty"`
	FailureReason   string     `json:"failure_reason,omitempty"`
	ValidationError string     `json:"validation_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
}

func (n Notification) Redacted() Notification {
	copy := n
	if copy.Message.Data != nil {
		copy.Message.Data = cloneStringMap(copy.Message.Data)
	}
	return copy
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
