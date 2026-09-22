package dnscheck

import (
	"context"
	"fmt"
	"strings"

	"github.com/mixeme/selfpost/internal/health"
)

// checkDKIM compares the TXT record published at <selector>._domainkey.<domain>
// with the key this server signs with. A wrong or absent record means every
// message fails DKIM at the receiver, so both are errors.
func (c *Checker) checkDKIM(ctx context.Context, q Query) Result {
	name := q.Selector + "._domainkey." + q.Name
	txt, found, err := c.lookupTXT(ctx, name)
	if err != nil {
		return lookupFailed("the DKIM record", err)
	}

	expected := publicKeyTag(q.ExpectedDKIM)
	if !found {
		return Result{
			Status: health.StatusError,
			Detail: fmt.Sprintf("No TXT record is published at %s. Publish the record shown above — until then every message fails DKIM.", name),
		}
	}

	for _, rec := range txt {
		got := publicKeyTag(rec)
		if got == "" {
			continue
		}
		if got == expected {
			return Result{
				Status:  health.StatusOK,
				Detail:  fmt.Sprintf("Published at %s and matching the key this server signs with.", name),
				Records: txt,
			}
		}
	}

	// Something is published, but it is not our key. Separate the revoked case
	// (empty p=), which reads as a deliberate act rather than a typo.
	for _, rec := range txt {
		if v, ok := tagValue(rec, "p"); ok && v == "" {
			return Result{
				Status:  health.StatusError,
				Detail:  fmt.Sprintf("The record at %s has an empty p= tag, which revokes the key. Replace it with the record shown above.", name),
				Records: txt,
			}
		}
	}
	return Result{
		Status:  health.StatusError,
		Detail:  fmt.Sprintf("A TXT record exists at %s but its public key is not the one this server signs with — mail will fail DKIM. Replace it with the record shown above (an old record from a previous server is the usual cause).", name),
		Records: txt,
	}
}

// checkDMARC reports whether the domain publishes a DMARC policy. DMARC is not
// required for delivery, so its absence is advice (warn), not a fault.
func (c *Checker) checkDMARC(ctx context.Context, q Query) Result {
	name := DMARCRecordName(q.Name)
	txt, found, err := c.lookupTXT(ctx, name)
	if err != nil {
		return lookupFailed("the DMARC record", err)
	}

	example := DMARCExample(q.DMARCReportEmail)

	var records []string
	for _, rec := range txt {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rec)), "v=dmarc1") {
			records = append(records, rec)
		}
	}
	if !found || len(records) == 0 {
		detail := fmt.Sprintf("No DMARC record at %s. Delivery works without one, but publishing %q tells receivers what to do with mail that fails authentication.", name, example)
		if q.DMARCReportEmail == "" {
			detail += " Aggregate reports (rua=) are optional on a send-only relay — omit rua= unless a mailbox that receives inbound mail is configured."
		}
		return Result{Status: health.StatusWarn, Detail: detail}
	}
	if len(records) > 1 {
		return Result{
			Status:  health.StatusError,
			Detail:  fmt.Sprintf("More than one DMARC record is published at %s. Receivers treat that as no policy at all — keep exactly one.", name),
			Records: records,
		}
	}

	policy, ok := tagValue(records[0], "p")
	if !ok || policy == "" {
		return Result{
			Status:  health.StatusWarn,
			Detail:  "A DMARC record is published but has no p= policy tag, so receivers ignore it. Add p=none, p=quarantine or p=reject.",
			Records: records,
		}
	}
	detail := fmt.Sprintf("Published with policy p=%s.", policy)
	if strings.EqualFold(policy, "none") {
		detail += " That is monitoring only — tighten it to quarantine or reject once the reports look clean."
	}
	return Result{Status: health.StatusOK, Detail: detail, Records: records}
}

// checkReportAuth verifies the hub domain publishes _report._dmarc for external
// aggregate-report destinations. Missing authorisation does not affect outbound
// delivery, only whether reports reach the rua= mailbox.
func (c *Checker) checkReportAuth(ctx context.Context, hubDomain string) Result {
	name := ReportAuthRecordName(hubDomain)
	expected := ReportAuthExample()
	txt, found, err := c.lookupTXT(ctx, name)
	if err != nil {
		return lookupFailed("the DMARC report-authorisation record", err)
	}

	var records []string
	for _, rec := range txt {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rec)), "v=dmarc1") {
			records = append(records, rec)
		}
	}
	if !found || len(records) == 0 {
		return Result{
			Status: health.StatusWarn,
			Detail: fmt.Sprintf("No report-authorisation record at %s. Aggregate DMARC reports sent to a mailbox on %s will not be delivered until %q is published there.", name, hubDomain, expected),
		}
	}
	if len(records) > 1 {
		return Result{
			Status:  health.StatusError,
			Detail:  fmt.Sprintf("More than one DMARC report-authorisation record is published at %s. Keep exactly one.", name),
			Records: records,
		}
	}
	return Result{
		Status:  health.StatusOK,
		Detail:  fmt.Sprintf("Published at %s — aggregate reports addressed to %s are authorised.", name, hubDomain),
		Records: records,
	}
}

// ReportAuth checks whether hubDomain authorises external DMARC aggregate
// reports. It is used on the settings page for the administrator profile.
func (c *Checker) ReportAuth(ctx context.Context, hubDomain string) Result {
	if hubDomain == "" {
		return Result{}
	}
	return c.checkReportAuth(ctx, hubDomain)
}

// publicKeyTag extracts the p= (public key) tag of a DKIM record, with all
// whitespace removed: DNS providers and TXT chunking freely insert spaces and
// line breaks into the base64, none of which are part of the key.
func publicKeyTag(record string) string {
	v, ok := tagValue(record, "p")
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(v), "")
}

// tagValue reads one tag from a DKIM/DMARC-style "tag=value; tag=value" record.
// Tag names are case-sensitive per RFC 6376/7489, and values keep their case.
func tagValue(record, tag string) (string, bool) {
	for _, part := range strings.Split(record, ";") {
		part = strings.TrimSpace(part)
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		if strings.TrimSpace(key) == tag {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}
