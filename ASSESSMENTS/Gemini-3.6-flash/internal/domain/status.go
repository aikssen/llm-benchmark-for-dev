package domain

type Status string

const (
	StatusPending         Status = "pending"
	StatusValidating      Status = "validating"
	StatusQueued          Status = "queued"
	StatusProcessing      Status = "processing"
	StatusDelivered       Status = "delivered"
	StatusFailed          Status = "failed"
	StatusPartiallyFailed Status = "partially_failed"
	StatusInvalid         Status = "invalid"
)

func (s Status) IsTerminal() bool {
	switch s {
	case StatusDelivered, StatusFailed, StatusPartiallyFailed, StatusInvalid:
		return true
	default:
		return false
	}
}
