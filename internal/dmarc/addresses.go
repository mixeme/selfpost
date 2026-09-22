package dmarc

import (
	"strings"
)

// DefaultLocalPart is the mailbox local-part for the panel-wide hosted address.
const DefaultLocalPart = "dmarc-reports"

// HostedReportAddress is the per-domain SelfPost-hosted rua= destination.
func HostedReportAddress(hostname, domain string) string {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	domain = strings.ToLower(strings.TrimSpace(domain))
	return DefaultLocalPart + "+" + domain + "@" + hostname
}

// DefaultHostedReportAddress is the settings-level hosted rua= when ingest is on.
func DefaultHostedReportAddress(hostname string) string {
	return DefaultLocalPart + "@" + strings.ToLower(strings.TrimSpace(hostname))
}

// IsHostedOnHostname reports whether addr is delivered locally on hostname.
func IsHostedOnHostname(addr, hostname string) bool {
	addr = strings.ToLower(strings.TrimSpace(addr))
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" || !strings.Contains(addr, "@") {
		return false
	}
	at := strings.LastIndex(addr, "@")
	return addr[at+1:] == hostname
}
