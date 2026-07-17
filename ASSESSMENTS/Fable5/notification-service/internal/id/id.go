// Package id generates unique, prefixed identifiers (e.g. "ntf_a1b2...").
// Prefixes make IDs self-describing in logs and API payloads.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a new random identifier with the given prefix, e.g.
// New("ntf") -> "ntf_1f6c9c6a...". It uses 16 bytes of crypto/rand entropy,
// which makes collisions practically impossible.
func New(prefix string) string {
	var b [16]byte
	// crypto/rand.Read never fails on supported platforms; it panics
	// internally if the system source is unavailable.
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b[:]))
}
