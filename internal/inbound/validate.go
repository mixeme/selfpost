package inbound

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

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
		return fmt.Errorf("domain is required")
	}
	if len(name) > 253 {
		return fmt.Errorf("domain must be at most 253 characters")
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must include at least one dot (e.g. example.com)")
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
		return fmt.Errorf("host is required")
	}
	if len(host) > 253 {
		return fmt.Errorf("host must be at most 253 characters")
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
		return fmt.Errorf("must not contain an empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("each label must be at most 63 characters")
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("labels must not start or end with '-'")
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '-' {
			return fmt.Errorf("may contain only lower-case letters, digits, '.' and '-'")
		}
	}
	return nil
}

func parsePort(raw string) (int, error) {
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

func checkTLSMode(mode string) error {
	switch mode {
	case "may", "encrypt", "none":
		return nil
	default:
		return fmt.Errorf("invalid TLS mode")
	}
}

func checkRecipientMode(mode string) error {
	switch mode {
	case "list", "any":
		return nil
	default:
		return fmt.Errorf("invalid recipient mode")
	}
}

func checkMailbox(addr, domain string) error {
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at >= len(addr)-1 {
		return fmt.Errorf("%q is not a valid email address", addr)
	}
	local, host := addr[:at], addr[at+1:]
	if host != domain {
		return fmt.Errorf("%q does not belong to domain %s", addr, domain)
	}
	if local == "" || local[0] == '.' || local[len(local)-1] == '.' {
		return fmt.Errorf("%q: invalid local part", addr)
	}
	for i := 0; i < len(local); i++ {
		c := local[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '.' && c != '-' && c != '_' && c != '+' {
			return fmt.Errorf("%q: local part contains invalid characters", addr)
		}
	}
	return nil
}
