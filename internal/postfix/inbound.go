package postfix

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mixeme/selfpost/internal/configsafe"
)

// InboundRoute is one inbound domain's Postfix map material: the relay
// domain, the next-hop transport, the TLS policy for that hop, and the
// recipient list (or a domain catch-all).
type InboundRoute struct {
	Domain        string
	Host          string
	Port          int
	TLSMode       string
	RecipientMode string
	Recipients    []string
}

func (p *Postfix) inboundMapPaths() (relayDomains, transport, recipients, tlsPolicy string) {
	dir := filepath.Dir(p.senderLoginMapsPath)
	return filepath.Join(dir, "relay_domains"),
		filepath.Join(dir, "transport"),
		filepath.Join(dir, "relay_recipients"),
		filepath.Join(dir, "tls_policy")
}

// RebuildInboundMaps regenerates the inbound relay lookup tables from the full
// set of configured routes and reloads Postfix. Domains with an empty host are
// omitted so mail is never accepted with nowhere to send it. Full regeneration
// keeps the files a pure function of the registry (security.md).
func (p *Postfix) RebuildInboundMaps(routes []InboundRoute) error {
	relay, transport, recipients, tlsPolicy, err := renderInboundMaps(routes)
	if err != nil {
		return err
	}
	rd, tr, rc, tl := p.inboundMapPaths()
	if err := writeFileAtomic(rd, relay, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(tr, transport, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(rc, recipients, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(tl, tlsPolicy, 0o640); err != nil {
		return err
	}
	return p.reload()
}

func renderInboundMaps(routes []InboundRoute) (relay, transport, recipients, tlsPolicy []byte, err error) {
	sort.Slice(routes, func(i, j int) bool { return routes[i].Domain < routes[j].Domain })

	var relayB, transportB, recipB, tlsB strings.Builder
	for _, r := range routes {
		if strings.TrimSpace(r.Host) == "" {
			continue
		}
		if err := assertInboundRouteSafe(r); err != nil {
			return nil, nil, nil, nil, err
		}
		nexthop := inboundNexthop(r.Host, r.Port)
		fmt.Fprintf(&relayB, "%s OK\n", r.Domain)
		fmt.Fprintf(&transportB, "%s smtp:%s\n", r.Domain, nexthop)
		fmt.Fprintf(&tlsB, "%s %s\n", nexthop, r.TLSMode)
		switch r.RecipientMode {
		case "any":
			fmt.Fprintf(&recipB, "@%s OK\n", r.Domain)
		default:
			addrs := append([]string(nil), r.Recipients...)
			sort.Strings(addrs)
			for _, addr := range addrs {
				fmt.Fprintf(&recipB, "%s OK\n", addr)
			}
		}
	}
	return []byte(relayB.String()), []byte(transportB.String()), []byte(recipB.String()), []byte(tlsB.String()), nil
}

// inboundNexthop is the Postfix next-hop [host]:port form. The brackets are
// what disable the MX lookup, so the operator's explicit upstream is used as
// given; they also delimit a bare IPv6 address.
func inboundNexthop(host string, port int) string {
	return "[" + host + "]:" + strconv.Itoa(port)
}

func assertInboundRouteSafe(r InboundRoute) error {
	if err := assertMapToken(r.Domain, "domain"); err != nil {
		return err
	}
	if err := assertMapToken(r.Host, "host"); err != nil {
		return err
	}
	if r.Port < 1 || r.Port > 65535 {
		return fmt.Errorf("postfix: invalid inbound port %d", r.Port)
	}
	switch r.TLSMode {
	case "may", "encrypt", "none":
	default:
		return fmt.Errorf("postfix: invalid tls mode %q", r.TLSMode)
	}
	if r.RecipientMode != "list" && r.RecipientMode != "any" {
		return fmt.Errorf("postfix: invalid recipient mode %q", r.RecipientMode)
	}
	for _, addr := range r.Recipients {
		if err := assertMapToken(addr, "recipient"); err != nil {
			return err
		}
	}
	return nil
}

// texthashForbidden is what may never appear in a texthash line: whitespace and
// the newline that would end it, the comma that separates values, and the
// backslash.
const texthashForbidden = " \t\r\n,\\"

// assertMapToken rejects values that could break out of a texthash line.
func assertMapToken(v, what string) error {
	if err := configsafe.Token(what, v, texthashForbidden); err != nil {
		return fmt.Errorf("postfix: %w", err)
	}
	return nil
}
