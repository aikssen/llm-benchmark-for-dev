package domain

import "strings"

// Channel identifies the medium used to deliver a notification.
//
// Channel is intentionally a plain string type rather than a closed enum:
// whether a channel is *supported* is decided at runtime by the set of
// registered providers, not by this package. Adding a new channel therefore
// requires no change here (see internal/provider).
type Channel string

// Channels supported by the providers shipped with the service. They are
// declared for convenience and documentation only; the authoritative list of
// supported channels is the provider registry.
const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

func (c Channel) String() string { return string(c) }

// ParseChannel normalises untrusted client input into a Channel.
func ParseChannel(s string) Channel {
	return Channel(strings.ToLower(strings.TrimSpace(s)))
}
