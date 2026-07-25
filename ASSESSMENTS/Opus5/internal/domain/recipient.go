package domain

// Recipient is the target of a single delivery.
type Recipient struct {
	// ID identifies the end user inside the platform. It is the key used for
	// preference lookups and rate limiting. It is optional: when it is absent
	// the Address is used instead, which keeps the API usable for one-off
	// transactional sends to addresses that have no user record.
	ID string `json:"id,omitempty"`

	// Address is the channel specific destination: an email address, a phone
	// number in E.164 format, a device push token, ...
	Address string `json:"address"`
}

// Key returns the stable identity used for preferences and rate limiting.
func (r Recipient) Key() string {
	if r.ID != "" {
		return r.ID
	}
	return r.Address
}
