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

// TaggedDomain returns the domain a hosted report address is tagged for
// (dmarc-reports+<domain>@<hostname> → <domain>), or "" when addr carries no
// such tag. Used to check a report's claimed domain against the address it
// actually arrived at.
func TaggedDomain(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	at := strings.LastIndex(addr, "@")
	if at <= 0 {
		return ""
	}
	local := addr[:at]
	plus := strings.Index(local, "+")
	if plus < 0 || local[:plus] != DefaultLocalPart {
		return ""
	}
	return local[plus+1:]
}
