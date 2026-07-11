package web

import (
	"fmt"
	"strings"
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

const maxDomainLen = 253 // RFC 1035 limit on a fully-qualified name

// normalizeDomain lower-cases and trims a domain name. Domain names are
// case-insensitive, and the generated OpenDKIM tables/keys use the canonical
// lower-case form, so we normalise before both validation and storage.
func normalizeDomain(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// validateDomain enforces a strict server-side whitelist for sending-domain
// names (spec 7.6.2). The result is safe to write verbatim into the OpenDKIM
// KeyTable/SigningTable and to use as a filesystem path segment: only
// lower-case letters, digits, '.' and '-' are allowed, in valid DNS label
// shape. Input must already be normalised with normalizeDomain.
//
// This is deliberately stricter than "any string DNS might accept" — no
// leading/trailing dots or hyphens, no empty or over-long labels, and at least
// two labels so single-word hostnames cannot be registered as sending domains.
func validateDomain(name string) error {
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
		if err := validateDomainLabel(label); err != nil {
			return err
		}
	}
	return nil
}

func validateDomainLabel(label string) error {
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
