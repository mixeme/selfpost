package web

import (
	"fmt"
	"unicode"
)

// minAdminPasswordLen is the floor for the administrator password. The panel is
// public (spec 7.6), so this is deliberately not tiny.
const minAdminPasswordLen = 12

const (
	minUsernameLen = 3
	maxUsernameLen = 64
)

// validateUsername enforces a strict server-side whitelist (spec 7.6.2):
// letters, digits, dot, dash, underscore. Client validation is never trusted.
func validateUsername(u string) error {
	if len(u) < minUsernameLen || len(u) > maxUsernameLen {
		return fmt.Errorf("username must be %d-%d characters", minUsernameLen, maxUsernameLen)
	}
	for _, r := range u {
		if r > unicode.MaxASCII || (!isASCIILetterOrDigit(r) && r != '.' && r != '-' && r != '_') {
			return fmt.Errorf("username may contain only letters, digits, '.', '-' and '_'")
		}
	}
	return nil
}

// validateAdminPassword enforces a minimum length. Composition rules beyond
// length tend to reduce entropy in practice, so length is the sole gate.
func validateAdminPassword(p string) error {
	if len(p) < minAdminPasswordLen {
		return fmt.Errorf("password must be at least %d characters", minAdminPasswordLen)
	}
	return nil
}

func isASCIILetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
