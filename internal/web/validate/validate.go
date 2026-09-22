// Package validate holds shared server-side form validation for the panel.
package validate

import (
	"fmt"
	"strings"
	"unicode"
)

// MinAdminPasswordLen is the floor for the administrator password. The panel is
// public (security.md), so this is deliberately not tiny.
const MinAdminPasswordLen = 12

const (
	minUsernameLen = 3
	maxUsernameLen = 64
)

// MinSecretFilePasswordLen is the floor for the password protecting an
// encrypted backup or domain export. Such a file is offline and can be attacked
// at leisure, so the floor matches the administrator password's rather than the
// weaker "any password is better than none".
const MinSecretFilePasswordLen = MinAdminPasswordLen

// Username enforces a strict server-side whitelist (security.md): letters,
// digits, dot, dash, underscore. Client validation is never trusted.
func Username(u string) error {
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

// AdminPassword enforces a minimum length. Composition rules beyond length tend
// to reduce entropy in practice, so length is the sole gate.
func AdminPassword(p string) error {
	if len(p) < MinAdminPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinAdminPasswordLen)
	}
	return nil
}

func isASCIILetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

const maxDomainLen = 253 // RFC 1035 limit on a fully-qualified name

// NormalizeDomain lower-cases and trims a domain name. Domain names are
// case-insensitive, and the generated OpenDKIM tables/keys use the canonical
// lower-case form, so we normalise before both validation and storage.
func NormalizeDomain(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Domain enforces a strict server-side whitelist for sending-domain names
// (security.md). The result is safe to write verbatim into the OpenDKIM
// KeyTable/SigningTable and to use as a filesystem path segment: only
// lower-case letters, digits, '.' and '-' are allowed, in valid DNS label
// shape. Input must already be normalised with NormalizeDomain.
func Domain(name string) error {
	if name == "" {
		return fmt.Errorf("domain is required")
	}
	if len(name) > maxDomainLen {
		return fmt.Errorf("domain must be at most %d characters", maxDomainLen)
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must include at least one dot (e.g. example.com)")
	}
	for _, label := range labels {
		if err := domainLabel(label); err != nil {
			return err
		}
	}
	return nil
}

func domainLabel(label string) error {
	if len(label) == 0 {
		return fmt.Errorf("domain must not contain an empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("each domain label must be at most 63 characters")
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("domain labels must not start or end with '-'")
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '-' {
			return fmt.Errorf("domain may contain only lower-case letters, digits, '.' and '-'")
		}
	}
	return nil
}

// freemailDomains lists public mail hosts that cannot publish _report._dmarc
// authorisation for third-party sending domains.
var freemailDomains = map[string]struct{}{
	"gmail.com":      {},
	"googlemail.com": {},
	"outlook.com":    {},
	"hotmail.com":    {},
	"live.com":       {},
	"yahoo.com":      {},
	"icloud.com":     {},
	"me.com":         {},
	"proton.me":      {},
	"protonmail.com": {},
}

// Email checks a DMARC rua= mailbox. Empty is allowed (policy-only).
func Email(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at >= len(addr)-1 {
		return fmt.Errorf("enter a valid email address")
	}
	local := addr[:at]
	domain := NormalizeDomain(addr[at+1:])
	if err := Domain(domain); err != nil {
		return fmt.Errorf("email domain is invalid: %w", err)
	}
	for _, r := range local {
		if r > unicode.MaxASCII {
			return fmt.Errorf("email address must be ASCII")
		}
		if !isASCIILetterOrDigit(r) && r != '.' && r != '-' && r != '_' && r != '+' {
			return fmt.Errorf("email address contains invalid characters")
		}
	}
	if _, blocked := freemailDomains[domain]; blocked {
		return fmt.Errorf("use an address on a domain you control; public mail hosts cannot receive authorised DMARC reports")
	}
	return nil
}
