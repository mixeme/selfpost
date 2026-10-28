package dnscheck

import "strings"

// SPF and DMARC are the two records SelfPost cannot generate for the operator:
// unlike the DKIM record they are policy, not a key, and a domain may already
// publish one for other senders. The panel still has to say what "correct"
// looks like, and the checks below have to suggest the same thing when a record
// is missing — so both take their example from here rather than each spelling
// out its own.

// DMARCRecordName is the name a DMARC record is published at. (SPF has no such
// helper: it is published at the domain itself.)
func DMARCRecordName(domainName string) string { return "_dmarc." + domainName }

// ReportAuthRecordName is where hubDomain, the domain of a report address,
// authorises aggregate reports about sendingDomain: a receiver looks up
// "<sending domain>._report._dmarc.<hub>" (RFC 7489 §7.1) - the sending
// domain first, so a hub says yes to each domain it takes reports for. Until
// 2.16.11 the panel named "_report._dmarc.<hub>", which no receiver asks for
// (2026-10-28, found adding dev.mixdep.ru with its reports on selfpost2).
func ReportAuthRecordName(sendingDomain, hubDomain string) string {
	return sendingDomain + "._report._dmarc." + hubDomain
}

// ReportAuthExample is the TXT value a hub domain publishes to accept reports.
func ReportAuthExample() string { return "v=DMARC1;" }

// SPFExample is the SPF record this server expects for a sending domain: its
// own name, which receivers resolve to the addresses its mail leaves from, and
// "-all" to say that nothing else is authorised. The name rather than the
// addresses, so that moving the server to another address changes the name's
// A record only and no domain's SPF (owner, 2026-10-26); the name has to
// resolve here anyway, for the PTR and the greeting.
func SPFExample(hostname string) string {
	return "v=spf1 a:" + hostname + " -all"
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
	return ReportAuthRecordName(sendingDomain, hub), ReportAuthExample(), true
}
