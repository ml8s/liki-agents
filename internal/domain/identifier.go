package domain

import "unicode"

// MaxIdentifierLength bounds untrusted identifiers before they reach ADK,
// audit storage, tracing, and in-memory lifecycle maps.
const MaxIdentifierLength = 128

// ValidIdentifier accepts bounded printable identifiers while preserving
// provider-specific identifier alphabets. It is the single definition shared by
// the runtime and the transport boundary.
func ValidIdentifier(value string) bool {
	if value == "" || len(value) > MaxIdentifierLength {
		return false
	}
	for _, char := range value {
		if !unicode.IsPrint(char) {
			return false
		}
	}
	return true
}
