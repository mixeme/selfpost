package dnscheck

import (
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

// DMARCExample is the least a domain should publish: monitoring only, with an
// address the aggregate reports go to. p=none is deliberate — it changes
// nothing about delivery, so it is safe to publish before the reports have
// shown that DKIM and SPF pass everywhere.
func DMARCExample(domainName string) string {
	return "v=DMARC1; p=none; rua=mailto:dmarc@" + domainName
}
