// Package validate holds shared server-side form validation for the panel.
package validate

import (
	"fmt"
	"net"
	"strconv"
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

const maxHostLen = 253

// NormalizeHost trims, lower-cases, and strips wrapping IPv6 brackets so the
// stored value is a bare hostname or IP, safe to wrap again when writing maps.
func NormalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host
}

// Host enforces a whitelist for an upstream hostname or IP (security.md): a
// dotted domain, a single DNS label (LAN names), or an IPv4/IPv6 address.
func Host(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if len(host) > maxHostLen {
		return fmt.Errorf("host must be at most %d characters", maxHostLen)
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if err := domainLabel(label); err != nil {
			return fmt.Errorf("host is invalid: %w", err)
		}
	}
	return nil
}

// Port checks a TCP port number parsed from form input.
func Port(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("port is required")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return n, nil
}

// TLSMode checks a Postfix smtp_tls_policy_maps level.
func TLSMode(mode string) error {
	switch mode {
	case "may", "encrypt", "none":
		return nil
	default:
		return fmt.Errorf("invalid TLS mode")
	}
}

// RecipientMode checks an inbound-domain recipient policy.
func RecipientMode(mode string) error {
	switch mode {
	case "list", "any":
		return nil
	default:
		return fmt.Errorf("invalid recipient mode")
	}
}

// MailboxInDomain checks that addr is a conservative mailbox on domain
// (security.md). domain must already be normalised.
func MailboxInDomain(addr, domain string) error {
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at >= len(addr)-1 {
		return fmt.Errorf("%q is not a valid email address", addr)
	}
	local, host := addr[:at], addr[at+1:]
	if host != domain {
		return fmt.Errorf("%q does not belong to domain %s", addr, domain)
	}
	if err := mailboxLocalPart(local); err != nil {
		return fmt.Errorf("%q: %w", addr, err)
	}
	return nil
}

func mailboxLocalPart(local string) error {
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
