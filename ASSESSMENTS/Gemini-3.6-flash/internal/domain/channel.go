package domain

import "strings"

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

func (c Channel) IsValid() bool {
	switch Channel(strings.ToLower(string(c))) {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return true
	default:
		return false
	}
}

func (c Channel) Normalized() Channel {
	return Channel(strings.ToLower(string(c)))
}
