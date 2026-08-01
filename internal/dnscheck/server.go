package dnscheck

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/mix/selfpost/internal/health"
)

// checkServer resolves the panel's own hostname and confirms the reverse
// lookup of each address points back at that name (FCrDNS). A missing or
// mismatched PTR is the single most common reason mail from a self-hosted
// server is rejected or scored as spam, which is why it is an error and not
// advice.
func (c *Checker) checkServer(ctx context.Context, hostname string) Server {
	srv := Server{Hostname: hostname, CheckedAt: time.Now()}
	if hostname == "" {
		srv.PTR = Result{
			Status: health.StatusUnknown,
			Detail: "SELFPOST_HOSTNAME is not set, so the server's own name in DNS cannot be checked. Set it in the deployment environment.",
		}
		return srv
	}

	addrs, err := c.resolver.LookupIPAddr(ctx, hostname)
	if err != nil || len(addrs) == 0 {
		srv.PTR = Result{
			Status: health.StatusError,
			Detail: fmt.Sprintf("%s does not resolve to any address. Publish an A (or AAAA) record for it — receiving servers check the name this server announces in HELO.", hostname),
		}
		return srv
	}

	want := normalizeName(hostname)
	matched, total := 0, len(addrs)
	var records []string
	for _, a := range addrs {
		ip := a.IP.String()
		srv.IPs = append(srv.IPs, ip)

		names, err := c.resolver.LookupAddr(ctx, ip)
		if err != nil || len(names) == 0 {
			records = append(records, ip+" → no PTR record")
			continue
		}
		hit := false
		for _, n := range names {
			if normalizeName(n) == want {
				hit = true
			}
		}
		if hit {
			matched++
			records = append(records, ip+" → "+normalizeName(names[0]))
		} else {
			records = append(records, ip+" → "+normalizeName(names[0])+" (does not match)")
		}
	}

	srv.PTR.Records = records
	switch {
	case matched == total:
		srv.PTR.Status = health.StatusOK
		srv.PTR.Detail = fmt.Sprintf("%s resolves to %s and the reverse lookup points back at it.", hostname, joinIPs(srv.IPs))
	case matched > 0:
		srv.PTR.Status = health.StatusWarn
		srv.PTR.Detail = fmt.Sprintf("Only %d of %d addresses of %s have a matching PTR record. Mail sent from the others may be rejected — set the reverse DNS of every address at your hosting provider.", matched, total, hostname)
	default:
		srv.PTR.Status = health.StatusError
		srv.PTR.Detail = fmt.Sprintf("No address of %s has a reverse (PTR) record pointing back at it. Many receiving servers reject or spam-score mail from such a host — set the reverse DNS of the server's IP to %s at your hosting provider.", hostname, hostname)
	}
	return srv
}

func joinIPs(ips []string) string {
	switch len(ips) {
	case 0:
		return "no address"
	case 1:
		return ips[0]
	default:
		out := ips[0]
		for _, ip := range ips[1:] {
			out += ", " + ip
		}
		return out
	}
}
