// Package id generates the service's public identifiers.
//
// Prefixed, URL-safe, 128 bits of entropy: enough to be unguessable and to
// avoid coordination, while staying readable in logs. A dependency on a UUID
// library would buy nothing over this.
package id

import (
	"crypto/rand"
	"encoding/hex"
)

// Prefixes used by the service.
const (
	PrefixNotification = "ntf"
	PrefixDelivery     = "dlv"
)

// New returns an identifier of the form "<prefix>_<32 hex chars>".
//
// It panics if the system CSPRNG fails, which on a healthy host does not
// happen and, if it did, would mean the process cannot safely continue.
func New(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("id: cannot read from the system random source: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}

// Notification returns a new notification identifier.
func Notification() string { return New(PrefixNotification) }

// Delivery returns a new delivery identifier.
func Delivery() string { return New(PrefixDelivery) }
