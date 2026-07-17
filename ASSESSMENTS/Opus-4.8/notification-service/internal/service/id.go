package service

import (
	"crypto/rand"
	"encoding/hex"
)

// newID returns a random 128-bit identifier as a 32-char hex string. crypto/rand
// makes collisions astronomically unlikely, so we avoid a UUID dependency while
// keeping IDs opaque and unguessable. rand.Read never fails on supported
// platforms; if it ever did we would rather panic than emit a predictable ID.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("service: cannot generate id: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
