package domain

// BatchRequest is the normalized, channel-agnostic representation of an
// incoming "submit notification" request, after the API layer has decoded
// and shape-checked the raw JSON. One BatchRequest fans out into one
// Notification per recipient.
type BatchRequest struct {
	Channel    Channel
	Message    Message
	Recipients []string
}
