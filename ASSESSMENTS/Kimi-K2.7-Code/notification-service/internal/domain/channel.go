package domain

import (
	"errors"
	"strings"
)

// Channel represents a supported notification channel.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

var (
	ErrUnsupportedChannel = errors.New("unsupported notification channel")
	validChannels                 = map[Channel]struct{}{
		ChannelEmail: {},
		ChannelSMS:   {},
		ChannelPush:  {},
	}
)

// ParseChannel converts a raw string into a Channel and validates it.
func ParseChannel(raw string) (Channel, error) {
	c := Channel(strings.ToLower(strings.TrimSpace(raw)))
	if _, ok := validChannels[c]; !ok {
		return "", ErrUnsupportedChannel
	}
	return c, nil
}

// String satisfies fmt.Stringer.
func (c Channel) String() string {
	return string(c)
}
