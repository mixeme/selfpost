package postfix

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// DMARCMapsConfig is the allow-list material for DMARC aggregate ingest.
type DMARCMapsConfig struct {
	Recipients []string
	Domains    []string
}

func (p *Postfix) dmarcMapPaths() (recipients, transport, relayDomains string) {
	dir := filepath.Dir(p.senderLoginMapsPath)
	return filepath.Join(dir, "dmarc_recipients"),
		filepath.Join(dir, "dmarc_transport"),
		filepath.Join(dir, "dmarc_relay_domains")
}

// RebuildDMARCMaps regenerates DMARC ingest lookup tables. Empty config clears
// the files so port-25 ingest is off even if the listener stays up for inbound
// relay.
func (p *Postfix) RebuildDMARCMaps(cfg DMARCMapsConfig) error {
	recip, transport, relay, err := renderDMARCMaps(cfg)
	if err != nil {
		return err
	}
	rc, tr, rd := p.dmarcMapPaths()
	if err := writeFileAtomic(rc, recip, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(tr, transport, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(rd, relay, 0o640); err != nil {
		return err
	}
	return p.reload()
}

func renderDMARCMaps(cfg DMARCMapsConfig) (recipients, transport, relayDomains []byte, err error) {
	recipients = []byte{}
	transport = []byte{}
	relayDomains = []byte{}
	if len(cfg.Recipients) == 0 {
		return recipients, transport, relayDomains, nil
	}
	addrs := append([]string(nil), cfg.Recipients...)
	sort.Strings(addrs)
	var recipB, transportB strings.Builder
	for _, addr := range addrs {
		if err := assertMapToken(addr, "recipient"); err != nil {
			return nil, nil, nil, err
		}
		fmt.Fprintf(&recipB, "%s OK\n", addr)
		fmt.Fprintf(&transportB, "%s dmarc-ingest:\n", addr)
	}
	domains := append([]string(nil), cfg.Domains...)
	sort.Strings(domains)
	var relayB strings.Builder
	for _, d := range domains {
		if err := assertMapToken(d, "domain"); err != nil {
			return nil, nil, nil, err
		}
		fmt.Fprintf(&relayB, "%s OK\n", d)
	}
	return []byte(recipB.String()), []byte(transportB.String()), []byte(relayB.String()), nil
}
