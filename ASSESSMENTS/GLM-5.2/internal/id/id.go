// Package id provides collision-resistant identifier generation without
// external dependencies. It produces RFC 4122 v4 UUIDs using crypto/rand.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a new v4-style UUID string (e.g. "550e8400-e29b-41d4-a716-446655440000").
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should not fail in practice; if it does we cannot
		// generate safe IDs, so panicking during construction is preferable
		// to silently producing duplicates.
		panic(fmt.Sprintf("id: failed to read random bytes: %v", err))
	}
	// Set version (4) and variant (RFC 4122) bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
