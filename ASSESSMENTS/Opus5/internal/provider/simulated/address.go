package simulated

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Address rules live next to the provider that owns the channel: the shape of
// a valid destination is a property of the integration, not of the core model.

var (
	// e164 is the international phone number format accepted by SMS gateways.
	e164 = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)
	// pushToken keeps the check deliberately loose — real device tokens vary
	// by vendor — while still rejecting obvious junk.
	pushToken = regexp.MustCompile(`^[A-Za-z0-9_\-:.]{16,256}$`)
)

// ValidateEmail checks RFC 5322 addressability.
func ValidateEmail(address string) error {
	if strings.TrimSpace(address) != address || address == "" {
		return errors.New("must be a non-empty address without surrounding whitespace")
	}
	addr, err := mail.ParseAddress(address)
	if err != nil {
		return fmt.Errorf("must be a valid email address: %w", err)
	}
	if addr.Address != address {
		return errors.New("must be a bare email address, without a display name")
	}
	return nil
}

// ValidatePhone checks E.164 format, e.g. +14155550123.
func ValidatePhone(address string) error {
	if !e164.MatchString(address) {
		return errors.New("must be a phone number in E.164 format, e.g. +14155550123")
	}
	return nil
}

// ValidatePushToken checks the shape of a device token.
func ValidatePushToken(address string) error {
	if !pushToken.MatchString(address) {
		return errors.New("must be a device token of 16-256 characters (letters, digits, '_', '-', ':', '.')")
	}
	return nil
}
