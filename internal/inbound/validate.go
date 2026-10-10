package inbound

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ValidationError is a refusal of what was submitted: its text says what the
// administrator must fix and is written to be shown on the page. Any other
// error from this package (a failed write, a failed reload) is not, and the
// caller must not put its text in front of a user.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

func normalizeDomain(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host
}

func checkDomain(name string) error {
	if name == "" {
		return invalid("domain is required")
	}
	if len(name) > 253 {
		return invalid("domain must be at most 253 characters")
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return invalid("domain must include at least one dot (e.g. example.com)")
	}
	for _, label := range labels {
		if err := checkLabel(label); err != nil {
			return err
		}
	}
	return nil
}

func checkHost(host string) error {
	if host == "" {
		return invalid("host is required")
	}
	if len(host) > 253 {
		return invalid("host must be at most 253 characters")
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if err := checkLabel(label); err != nil {
			return fmt.Errorf("host is invalid: %w", err)
		}
	}
	return nil
}

func checkLabel(label string) error {
	if len(label) == 0 {
		return invalid("must not contain an empty label")
	}
	if len(label) > 63 {
		return invalid("each label must be at most 63 characters")
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return invalid("labels must not start or end with '-'")
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '-' {
			return invalid("may contain only lower-case letters, digits, '.' and '-'")
		}
	}
	return nil
}

func parsePort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, invalid("port is required")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		return 0, invalid("port must be between 1 and 65535")
	}
	return n, nil
}

func checkTLSMode(mode string) error {
	switch mode {
	case "may", "encrypt", "none":
		return nil
	default:
		return invalid("invalid TLS mode")
	}
}

func checkRecipientMode(mode string) error {
	switch mode {
	case "list", "any":
		return nil
	default:
		return invalid("invalid recipient mode")
	}
}

func checkMailbox(addr, domain string) error {
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at >= len(addr)-1 {
		return invalid("%q is not a valid email address", addr)
	}
	local, host := addr[:at], addr[at+1:]
	if host != domain {
		return invalid("%q does not belong to domain %s", addr, domain)
	}
	if local == "" || local[0] == '.' || local[len(local)-1] == '.' {
		return invalid("%q: invalid local part", addr)
	}
	for i := 0; i < len(local); i++ {
		c := local[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '.' && c != '-' && c != '_' && c != '+' {
			return invalid("%q: local part contains invalid characters", addr)
		}
	}
	return nil
}
