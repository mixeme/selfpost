package dnscheck

import (
	"database/sql"
	"net"
	"strings"
)

// SPF and DMARC are the two records SelfPost cannot generate for the operator:
// unlike the DKIM record they are policy, not a key, and a domain may already
// publish one for other senders. The panel still has to say what "correct"
// looks like, and the checks below have to suggest the same thing when a record
// is missing — so both take their example from here rather than each spelling
// out its own.

// DMARCRecordName is the name a DMARC record is published at. (SPF has no such
// helper: it is published at the domain itself.)
func DMARCRecordName(domainName string) string { return "_dmarc." + domainName }

// ReportAuthRecordName is where a report-receiving domain authorises external
// DMARC aggregate destinations (RFC 7489 §7.1).
func ReportAuthRecordName(hubDomain string) string { return "_report._dmarc." + hubDomain }

// ReportAuthExample is the TXT value a hub domain publishes to accept reports.
func ReportAuthExample() string { return "v=DMARC1;" }

// SPFExample is the SPF record this server expects for a sending domain: the
// addresses its mail actually leaves from, and "-all" to say that nothing else
// is authorised. When the server's own addresses are not known (its hostname
// does not resolve) it falls back to an "a:" mechanism naming the host, so the
// panel always has something concrete to show.
func SPFExample(hostname string, serverIPs []string) string {
	var mechanisms []string
	for _, s := range serverIPs {
		ip := net.ParseIP(strings.TrimSpace(s))
		switch {
		case ip == nil:
			continue
		case ip.To4() != nil:
			mechanisms = append(mechanisms, "ip4:"+ip.String())
		default:
			mechanisms = append(mechanisms, "ip6:"+ip.String())
		}
	}
	if len(mechanisms) == 0 {
		mechanisms = []string{"a:" + hostname}
	}
	return "v=spf1 " + strings.Join(mechanisms, " ") + " -all"
}

// ResolveDMARCRua picks the rua= mailbox for a sending domain: per-domain
// override wins, then the administrator profile, then policy-only (empty).
func ResolveDMARCRua(domainRua sql.NullString, profileEmail string) string {
	if domainRua.Valid {
		return domainRua.String
	}
	return profileEmail
}

// EmailDomain returns the lower-case domain part of addr, or "" when invalid.
func EmailDomain(addr string) string {
	addr = strings.TrimSpace(addr)
	at := strings.LastIndex(addr, "@")
	if at < 0 || at == len(addr)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr[at+1:]))
}

// DMARCExample is the DMARC TXT record this server suggests for a sending
// domain. p=none is deliberate — it changes nothing about delivery. rua= is
// included only when reportEmail is set; SelfPost is send-only and most
// operators have no inbox on the sending domain itself.
func DMARCExample(reportEmail string) string {
	base := "v=DMARC1; p=none"
	if reportEmail == "" {
		return base
	}
	return base + "; rua=mailto:" + reportEmail
}

// ExternalReportAuth reports whether the hub domain must publish a
// _report._dmarc authorisation for aggregate reports sent to reportEmail from
// sendingDomain.
func ExternalReportAuth(sendingDomain, reportEmail string) (name, value string, ok bool) {
	hub := EmailDomain(reportEmail)
	if hub == "" || strings.EqualFold(hub, sendingDomain) {
		return "", "", false
	}
	return ReportAuthRecordName(hub), ReportAuthExample(), true
}
