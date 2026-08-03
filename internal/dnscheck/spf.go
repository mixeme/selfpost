package dnscheck

import (
	"context"
	"fmt"
	"net"
	"strings"

	"codeberg.org/mix/selfpost/internal/health"
)

// spfLookupBudget caps how many DNS lookups one SPF evaluation may spend on
// a/mx mechanisms. RFC 7208 allows a receiver ten; staying under the same
// ceiling keeps a hostile or careless record from turning a page view into a
// long chain of queries.
const spfLookupBudget = 10

// checkSPF reports whether the domain's SPF record authorises this server.
//
// This is deliberately a shallow check (documented as such in the README): it
// looks for a mechanism that literally covers the server's address —
// ip4:/ip6:, or a/mx resolving to it — and does not recurse into include: or
// redirect=, nor evaluate the record the way a receiver would. That is why a
// record which does not obviously cover us but does use include: is reported as
// a warning ("cannot tell") rather than a failure.
func (c *Checker) checkSPF(ctx context.Context, q Query) Result {
	ips := parseIPs(q.ServerIPs)
	if len(ips) == 0 {
		return Result{
			Status: health.StatusUnknown,
			Detail: "The server's own address is not known (its hostname does not resolve), so SPF coverage cannot be checked. Fix the hostname/PTR check first.",
		}
	}

	txt, found, err := c.lookupTXT(ctx, q.Name)
	if err != nil {
		return lookupFailed("the SPF record", err)
	}

	var records []string
	for _, rec := range txt {
		if isSPF(rec) {
			records = append(records, strings.TrimSpace(rec))
		}
	}
	switch {
	case !found || len(records) == 0:
		return Result{
			Status: health.StatusError,
			Detail: fmt.Sprintf("No SPF record is published for %s. Publish a TXT record such as %q — without it receivers have nothing authorising this server to send as the domain.", q.Name, SPFExample(q.Hostname, q.ServerIPs)),
		}
	case len(records) > 1:
		return Result{
			Status:  health.StatusError,
			Detail:  fmt.Sprintf("More than one SPF record is published for %s. RFC 7208 allows exactly one; receivers treat several as a permanent error and the domain gets no SPF pass at all. Merge them into a single record.", q.Name),
			Records: records,
		}
	}

	matched, unfollowed := c.evaluateSPF(ctx, records[0], q.Name, ips)
	switch {
	case matched == "+all" || matched == "all":
		return Result{
			Status:  health.StatusWarn,
			Detail:  "The SPF record ends with \"+all\", which authorises every server on the internet to send as this domain. Replace it with an explicit ip4:/ip6: or a mechanism plus \"-all\".",
			Records: records,
		}
	case matched != "":
		return Result{
			Status:  health.StatusOK,
			Detail:  fmt.Sprintf("The SPF record authorises this server through its %q mechanism.", matched),
			Records: records,
		}
	case len(unfollowed) > 0:
		return Result{
			Status: health.StatusWarn,
			Detail: fmt.Sprintf("No mechanism in the SPF record lists %s directly, but the record uses %s, which this check does not follow — the server may still be authorised through it. Verify with an external SPF validator, or add \"ip4:%s\" to be sure.",
				ips[0], strings.Join(unfollowed, ", "), ips[0]),
			Records: records,
		}
	default:
		return Result{
			Status:  health.StatusError,
			Detail:  fmt.Sprintf("The SPF record does not authorise %s, so mail sent from this server fails SPF. Add \"ip4:%s\" (or an \"a\" mechanism resolving here) to the record.", ips[0], ips[0]),
			Records: records,
		}
	}
}

// evaluateSPF walks the record's mechanisms, returning the first one that
// covers one of the server's addresses, plus the mechanisms this shallow check
// cannot resolve (include:/redirect=/exists:/ptr and anything past the lookup
// budget) so the caller can say "cannot tell" instead of "fails".
func (c *Checker) evaluateSPF(ctx context.Context, record, domainName string, ips []net.IP) (matched string, unfollowed []string) {
	budget := spfLookupBudget
	seenUnfollowed := make(map[string]bool)
	note := func(kind string) {
		if !seenUnfollowed[kind] {
			seenUnfollowed[kind] = true
			unfollowed = append(unfollowed, kind)
		}
	}

	terms := strings.Fields(record)
	if len(terms) > 0 {
		terms = terms[1:] // drop the v=spf1 version token
	}
	for _, term := range terms {
		qualifier, mech := splitQualifier(term)
		lower := strings.ToLower(mech)
		name, hasArg := mechanismArg(mech)

		switch {
		case strings.HasPrefix(lower, "ip4:"), strings.HasPrefix(lower, "ip6:"):
			if qualifier != '+' {
				continue
			}
			if coversAny(mech[4:], ips) {
				return term, unfollowed
			}

		case lower == "a" || strings.HasPrefix(lower, "a:") || strings.HasPrefix(lower, "a/"):
			if strings.Contains(mech, "/") { // prefix-length form: not evaluated
				note("a/<prefix>")
				continue
			}
			target := domainName
			if hasArg {
				target = name
			}
			if budget <= 0 {
				note("further lookups")
				continue
			}
			budget--
			if qualifier == '+' && c.resolvesTo(ctx, target, ips) {
				return term, unfollowed
			}

		case lower == "mx" || strings.HasPrefix(lower, "mx:") || strings.HasPrefix(lower, "mx/"):
			if strings.Contains(mech, "/") {
				note("mx/<prefix>")
				continue
			}
			target := domainName
			if hasArg {
				target = name
			}
			if budget <= 0 {
				note("further lookups")
				continue
			}
			budget--
			if qualifier == '+' && c.mxResolvesTo(ctx, target, ips, &budget) {
				return term, unfollowed
			}

		case strings.HasPrefix(lower, "include:"):
			note("include:")
		case strings.HasPrefix(lower, "redirect="):
			note("redirect=")
		case strings.HasPrefix(lower, "exists:"):
			note("exists:")
		case lower == "ptr" || strings.HasPrefix(lower, "ptr:"):
			note("ptr")

		case lower == "all":
			if qualifier == '+' {
				return "+all", unfollowed
			}
			// "-all"/"~all"/"?all" terminates the record: nothing after it is
			// evaluated by a receiver either.
			return "", unfollowed
		}
	}
	return "", unfollowed
}

// resolvesTo reports whether name resolves to one of the server's addresses.
func (c *Checker) resolvesTo(ctx context.Context, name string, ips []net.IP) bool {
	addrs, err := c.resolver.LookupIPAddr(ctx, name)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		for _, ip := range ips {
			if a.IP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// mxResolvesTo reports whether any of name's MX hosts resolves to one of the
// server's addresses, spending at most the remaining lookup budget.
func (c *Checker) mxResolvesTo(ctx context.Context, name string, ips []net.IP, budget *int) bool {
	mxs, err := c.resolver.LookupMX(ctx, name)
	if err != nil {
		return false
	}
	for _, mx := range mxs {
		if *budget <= 0 {
			return false
		}
		*budget--
		if c.resolvesTo(ctx, strings.TrimSuffix(mx.Host, "."), ips) {
			return true
		}
	}
	return false
}

// coversAny reports whether an ip4:/ip6: value — a bare address or a CIDR —
// contains one of the server's addresses.
func coversAny(value string, ips []net.IP) bool {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return false
		}
		for _, ip := range ips {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	listed := net.ParseIP(value)
	if listed == nil {
		return false
	}
	for _, ip := range ips {
		if listed.Equal(ip) {
			return true
		}
	}
	return false
}

// splitQualifier peels the optional +/-/~/? qualifier off a mechanism,
// defaulting to "+" (pass) as RFC 7208 does.
func splitQualifier(term string) (byte, string) {
	if term == "" {
		return '+', ""
	}
	switch term[0] {
	case '+', '-', '~', '?':
		return term[0], term[1:]
	default:
		return '+', term
	}
}

// mechanismArg returns the ":" argument of a mechanism, if it has one.
func mechanismArg(mech string) (string, bool) {
	_, arg, found := strings.Cut(mech, ":")
	if !found || arg == "" {
		return "", false
	}
	return arg, true
}

// isSPF reports whether a TXT record is an SPF record (the version token must
// be the whole first term, so "v=spf10" is not one).
func isSPF(record string) bool {
	rec := strings.TrimSpace(record)
	if len(rec) < 6 || !strings.EqualFold(rec[:6], "v=spf1") {
		return false
	}
	return len(rec) == 6 || rec[6] == ' ' || rec[6] == '\t'
}

// parseIPs converts the string addresses carried on a Query back into net.IPs,
// dropping anything unparsable.
func parseIPs(in []string) []net.IP {
	var ips []net.IP
	for _, s := range in {
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}
