// Package configsafe holds the defence-in-depth check that every generated
// configuration file shares: a value about to be written into a table line must
// be non-empty and must not carry a character that could end the field, end the
// line, or otherwise change what the file means (security.md).
//
// The forbidden set stays with the caller rather than living here, because what
// breaks a line differs per format — a comma separates values in a Postfix map,
// a colon separates fields in an OpenDKIM table — and the set belongs next to
// the writer it protects, where a reviewer of that writer can see it.
package configsafe

import (
	"fmt"
	"strings"
)

// Token reports whether value is safe to interpolate into a config line. what
// names the field for the error message ("domain", "login", …) and forbidden
// lists the characters the format cannot carry.
func Token(what, value, forbidden string) error {
	if value == "" {
		return fmt.Errorf("empty %s", what)
	}
	if strings.ContainsAny(value, forbidden) {
		return fmt.Errorf("unsafe character in %s %q", what, value)
	}
	return nil
}
