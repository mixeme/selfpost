package app

import (
	"fmt"
	"strings"

	"codeberg.org/mix/selfpost/internal/store"
)

const (
	minLoginLen = 3
	maxLoginLen = 64
)

// validateLogin enforces a strict server-side whitelist for the SASL login
// (spec 7.6.2). It intentionally excludes '@': the login is stored in sasldb2,
// where '@' separates the user from the realm, so allowing it would change the
// account's identity. Client validation is never trusted.
//
// The login is the one piece of user input that is passed to saslpasswd2 as a
// command argument (never through a shell, spec 7.6.3); this whitelist is what
// makes that safe.
func validateLogin(login string) error {
	if len(login) < minLoginLen || len(login) > maxLoginLen {
		return fmt.Errorf("login must be %d-%d characters", minLoginLen, maxLoginLen)
	}
	for _, r := range login {
		lower := r >= 'a' && r <= 'z'
		upper := r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if !lower && !upper && !digit && r != '.' && r != '-' && r != '_' {
			return fmt.Errorf("login may contain only letters, digits, '.', '-' and '_'")
		}
	}
	return nil
}

// validateImportedPassword guards a password taken from a domain-export file
// (spec 7.5.B) before it is written to sasldb2. Our own exports carry base64url
// passwords, but the file is untrusted input, so we reject an empty value or one
// containing control characters — saslpasswd2 reads the passphrase from stdin
// and a newline would silently truncate it (spec 7.6.2).
func validateImportedPassword(password string) error {
	if password == "" {
		return fmt.Errorf("imported application password is empty")
	}
	if len(password) > 1024 {
		return fmt.Errorf("imported application password is too long")
	}
	for _, r := range password {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("imported application password contains control characters")
		}
	}
	return nil
}

// validateAddressMode checks the submitted mode is one of the two known values.
func validateAddressMode(mode string) error {
	if mode != store.AddressModeWildcard && mode != store.AddressModeList {
		return fmt.Errorf("invalid address mode")
	}
	return nil
}

// normalizeAddress lower-cases and trims a sender address. Both the local part
// and domain are treated case-insensitively for the ownership check and for the
// generated map, matching how addresses are compared elsewhere.
func normalizeAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}

// validateSenderAddress enforces that a list-mode address is well-formed and,
// critically, belongs to the application's own domain (spec 7.6.2). The domain
// check is done here, before anything is written to a config file — not left to
// smtpd_sender_login_maps to catch at delivery time. domain must already be a
// validated, normalised domain name.
func validateSenderAddress(addr, domain string) error {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return fmt.Errorf("%q is not a valid email address", addr)
	}
	local, host := addr[:at], addr[at+1:]
	if host != domain {
		return fmt.Errorf("%q does not belong to domain %s", addr, domain)
	}
	if err := validateLocalPart(local); err != nil {
		return fmt.Errorf("%q: %w", addr, err)
	}
	return nil
}

// validateLocalPart applies a conservative whitelist to the part before '@'.
// This is deliberately stricter than RFC 5321 (no quoted local parts) so the
// value is always safe to write verbatim into the Postfix map (spec 7.6.4).
func validateLocalPart(local string) error {
	if local == "" {
		return fmt.Errorf("missing the part before '@'")
	}
	if local[0] == '.' || local[len(local)-1] == '.' {
		return fmt.Errorf("local part must not start or end with '.'")
	}
	for i := 0; i < len(local); i++ {
		c := local[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '.' && c != '-' && c != '_' && c != '+' {
			return fmt.Errorf("local part may contain only lower-case letters, digits, '.', '-', '_' and '+'")
		}
	}
	return nil
}

// parseAddresses normalises, validates and de-duplicates a list of submitted
// sender addresses for a list-mode application. It requires at least one address
// and that each belongs to domain. The returned slice is de-duplicated but keeps
// submission order stable for display; the store sorts on read.
func parseAddresses(raw []string, domain string) ([]string, error) {
	seen := make(map[string]bool)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		addr := normalizeAddress(r)
		if addr == "" {
			continue
		}
		if err := validateSenderAddress(addr, domain); err != nil {
			return nil, err
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("list mode requires at least one address")
	}
	return out, nil
}
