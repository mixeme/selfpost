package e2e

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The stand starts with both port-25 features off, as a new installation does.
// The last part of the run switches them on and uses them from outside, the
// way a stranger's mail server would: until 2.0.1 the suite only read back the
// generated configuration, and DMARC ingest shipped in 1.7.0 with a delivery
// service Postfix could not start - nothing ever sent a report through it.

// testEnablePort25Features recreates the container with the inbound relay and
// DMARC ingest on, over the same /data.
func testEnablePort25Features(t *testing.T, sc *scenario) {
	h.env = append(h.env, "INBOUND_RELAY_ENABLE=true", "DMARC_REPORTS_ENABLE=true")
	if err := h.recreateSelfpost(); err != nil {
		t.Fatalf("recreate selfpost with the port-25 features on: %v", err)
	}
	if err := waitForPanelReady(); err != nil {
		t.Fatalf("panel after enabling the port-25 features: %v\n%s", err, h.logs("selfpost"))
	}
	if err := waitForSMTPSReady(60 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForPort25Ready(60 * time.Second); err != nil {
		t.Fatalf("%v\n%s", err, h.logs("selfpost"))
	}
	if err := checkSupervisorProcesses(h); err != nil {
		t.Fatal(err)
	}
	// Nothing in the container's own output may complain about the generated
	// configuration: the broken delivery service of 1.7.0-1.9.5 was announced
	// there at every start ("unmatched service_name/type") and went unread.
	logs := h.logs("selfpost")
	for _, bad := range []string{"unmatched service_name", "postconf: warning", "postconf: fatal"} {
		if strings.Contains(logs, bad) {
			t.Fatalf("the container log has %q with the port-25 features on:\n%s", bad, logs)
		}
	}
	resp, err := sc.panel.status()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request.URL.Path != "/overview" {
		t.Fatalf("the session did not survive the recreate: landed on %s", resp.Request.URL.Path)
	}
}

const reportReporter = "reporter.e2e.test"

// aggregateXML is a DMARC aggregate report on domain, as a receiver sends it.
func aggregateXML(domain, reportID string) string {
	return `<?xml version="1.0"?>
<feedback>
  <report_metadata>
    <org_name>` + reportReporter + `</org_name>
    <email>noreply@` + reportReporter + `</email>
    <report_id>` + reportID + `</report_id>
    <date_range><begin>1723593600</begin><end>1723680000</end></date_range>
  </report_metadata>
  <policy_published>
    <domain>` + domain + `</domain>
    <p>none</p><sp>none</sp><pct>100</pct>
    <adkim>r</adkim><aspf>r</aspf>
  </policy_published>
  <record>
    <row>
      <source_ip>203.0.113.10</source_ip>
      <count>7</count>
      <policy_evaluated>
        <disposition>none</disposition>
        <dkim>pass</dkim><spf>pass</spf>
      </policy_evaluated>
    </row>
    <identifiers><header_from>` + domain + `</header_from></identifiers>
  </record>
</feedback>
`
}

// reportMessage is the mail a receiver sends a report in: one gzip attachment,
// base64 in 76-column lines.
func reportMessage(t *testing.T, to, xml string) string {
	t.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(xml)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(gz.Bytes())
	var b strings.Builder
	fmt.Fprintf(&b, "From: noreply@%s\r\nTo: %s\r\nSubject: Report domain: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n",
		reportReporter, to, senderDomain, time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("Content-Type: application/gzip; name=\"report.xml.gz\"\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return b.String()
}

// testDMARCReportReachesThePanel gives the sending domain a SelfPost-hosted
// report address and mails a report to it on port 25. It has to show up under
// Outbound > DMARC reports.
func testDMARCReportReachesThePanel(t *testing.T, sc *scenario) {
	address := "dmarc-reports+" + senderDomain + "@" + selfpostHostname
	resp, body, err := sc.panel.postForm("/outbound/domains/"+sc.domainID+"/settings/reports", url.Values{"dmarc_rua_mode": {"hosted"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/outbound/domains/"+sc.domainID {
		t.Fatalf("choosing the hosted report address: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}

	// An address nobody chose is refused before DATA, hosted form or not.
	res := deliverPort25("noreply@"+reportReporter, "dmarc-reports+nobody.e2e.test@"+selfpostHostname, "Subject: x\r\n\r\nx\r\n")
	if res.rcptErr == nil {
		t.Fatalf("port 25 accepted a report address no domain uses (first error: %v)", res.firstErr())
	}

	res = deliverPort25("noreply@"+reportReporter, address, reportMessage(t, address, aggregateXML(senderDomain, "e2e-report-1")))
	if !res.ok() {
		t.Fatalf("port 25 did not accept a report at %s: %v", address, res.firstErr())
	}
	if err := waitFor("the report to appear under Outbound > DMARC reports", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		resp, body, err := sc.panel.get("/outbound/dmarc")
		if err != nil {
			return false, err
		}
		if resp.StatusCode != 200 {
			return false, fmt.Errorf("GET /outbound/dmarc = %d", resp.StatusCode)
		}
		if strings.Contains(body, reportReporter) {
			return true, nil
		}
		return false, fmt.Errorf("no report from %s on the page yet; mail log:\n%s", reportReporter, mailLogTail(h, "dmarc-ingest"))
	}); err != nil {
		t.Fatal(err)
	}
}

// testDMARCJunkIsDropped mails two things that are not reports this relay
// keeps to the same address: a report on somebody else's domain, and a message
// with no report in it. Each is accepted, dropped at once with the reason in
// the mail log, and counted; neither stays in the queue to be tried again and
// neither becomes a report.
func testDMARCJunkIsDropped(t *testing.T, sc *scenario) {
	address := "dmarc-reports+" + senderDomain + "@" + selfpostHostname
	junk := map[string]string{
		"a report on another domain": reportMessage(t, address, aggregateXML("stranger.e2e.test", "e2e-report-2")),
		"a message with no report":   "From: someone@stranger.e2e.test\r\nTo: " + address + "\r\nSubject: hello\r\n\r\nnot a report\r\n",
	}
	for what, msg := range junk {
		if res := deliverPort25("someone@stranger.e2e.test", address, msg); !res.ok() {
			t.Fatalf("%s was refused on port 25 instead of being accepted and dropped: %v", what, res.firstErr())
		}
	}

	dropped := regexp.MustCompile(`status=sent \(delivered via dmarc-ingest service \(dropped: [^)]`)
	if err := waitFor("both messages to be dropped", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		log := mailLogTail(h, "dmarc-ingest")
		if n := len(dropped.FindAllString(log, -1)); n != len(junk) {
			return false, fmt.Errorf("%d of %d dropped so far; mail log:\n%s", n, len(junk), log)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	queue, err := h.execIn("selfpost", "postqueue", "-p")
	if err != nil {
		t.Fatalf("postqueue -p: %v\n%s", err, queue)
	}
	if !strings.Contains(queue, "Mail queue is empty") {
		t.Fatalf("a dropped message is still in the queue:\n%s", queue)
	}

	_, body, err := sc.panel.get("/outbound/dmarc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "2 parse failures") {
		t.Fatalf("the DMARC page does not count the two dropped messages as parse failures:\n%s", body)
	}
	if strings.Contains(body, "stranger.e2e.test") {
		t.Fatal("the report on another domain was stored")
	}
}

// mailLogTail returns the lines of the end of the mail log that contain filter.
func mailLogTail(s *stack, filter string) string {
	out, err := s.execIn("selfpost", "tail", "-n", "400", "/data/log/mail.log")
	if err != nil {
		return "(mail log unreadable: " + err.Error() + ")"
	}
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, filter) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

const (
	inboundDomain    = "in.e2e.test"
	inboundRecipient = "user@" + inboundDomain
)

// testInboundRelay sets up an inbound domain with the sink as its upstream and
// one listed recipient, then talks to port 25 as a stranger: mail for the
// listed recipient reaches the upstream, mail for anyone else at the domain
// and for a domain that is not configured is refused before DATA.
func testInboundRelay(t *testing.T, sc *scenario) {
	resp, body, err := sc.panel.postForm("/inbound/domains", url.Values{"name": {inboundDomain}})
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^/inbound/domains/(\d+)$`).FindStringSubmatch(resp.Request.URL.Path)
	if m == nil {
		t.Fatalf("add inbound domain: landed on %s:\n%.800s", resp.Request.URL.Path, body)
	}
	base := "/inbound/domains/" + m[1]

	// The sink by address: an upstream named by an IP is delivered to as it
	// stands, with no MX lookup in between.
	resp, body, err = sc.panel.postForm(base+"/upstream", url.Values{"host": {"10.77.0.11"}, "port": {"25"}, "tls_mode": {"none"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != base {
		t.Fatalf("set the upstream: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	resp, body, err = sc.panel.postForm(base+"/recipients", url.Values{"recipient_mode": {"list"}, "addresses": {inboundRecipient}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != base {
		t.Fatalf("set the recipients: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}

	message := func(to, token string) string {
		return fmt.Sprintf("From: someone@stranger.e2e.test\r\nTo: %s\r\nSubject: e2e %s\r\nDate: %s\r\n\r\nmail for the inbound domain\r\n",
			to, token, time.Now().UTC().Format(time.RFC1123Z))
	}

	token := uniqueToken("inbound")
	if res := deliverPort25("someone@stranger.e2e.test", inboundRecipient, message(inboundRecipient, token)); !res.ok() {
		t.Fatalf("port 25 did not accept mail for %s: %v", inboundRecipient, res.firstErr())
	}
	if _, err := findSinkMessage(h.stageDir, token, 30*time.Second); err != nil {
		t.Fatalf("mail for the inbound domain never reached its upstream: %v\n%s", err, mailLogTail(h, "postfix/"))
	}

	// The inbound journal milter saw it go through. No page shows the journal
	// yet, so the row is read where it is kept. No filter is attached on the
	// stand: the row is an accept by nobody.
	// The filter lists exist as files from the first start with the relay on,
	// readable by a filter that is another user in another container. Nothing
	// is on them: no page edits them yet.
	maps, err := h.execIn("selfpost", "sh", "-c", `cd /data/antispam && stat -c "%a %n" . *.map && { grep -vc "^#" *.map; true; }`)
	if err != nil {
		t.Fatalf("list /data/antispam: %v\n%s", err, maps)
	}
	for _, want := range []string{"755 .",
		"644 allow_domain.map", "644 allow_from.map", "644 allow_ip.map",
		"644 deny_domain.map", "644 deny_from.map", "644 deny_ip.map",
		"allow_ip.map:0", "deny_from.map:0",
	} {
		if !strings.Contains(maps, want) {
			t.Fatalf("/data/antispam: no %q in\n%s", want, maps)
		}
	}

	// journalHas waits for a row and returns the journal as it then stands.
	journalHas := func(want string) string {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(time.Second) {
			rows, err := h.execIn("selfpost", "python3", "-c",
				`import sqlite3; print("\n".join("|".join(r) for r in sqlite3.connect("file:/data/selfpost.db?mode=ro", uri=True).execute("select inbound_domain, from_addr, to_addr, subject, decision, engine from inbound_spam_log")))`)
			if err != nil {
				t.Fatalf("read the inbound journal: %v\n%s", err, rows)
			}
			if strings.Contains(rows, want) {
				return strings.TrimSpace(rows)
			}
			if time.Now().After(deadline) {
				t.Fatalf("no journal row %q, journal:\n%s\n%s", want, rows, mailLogTail(h, "postfix/"))
			}
		}
	}
	rows := journalHas(fmt.Sprintf("%s|someone@stranger.e2e.test|%s|e2e %s|accept|selfpost", inboundDomain, inboundRecipient, token))
	// A report mailed to the server's own name earlier in the run came through
	// the same listener and is not the relay's mail.
	if n := strings.Count(rows, "\n") + 1; n != 1 {
		t.Fatalf("the journal holds %d rows, want the relayed message alone:\n%s", n, rows)
	}

	for _, to := range []string{"nobody@" + inboundDomain, "someone@elsewhere.e2e.test"} {
		res := deliverPort25("someone@stranger.e2e.test", to, message(to, uniqueToken("refused")))
		if res.rcptErr == nil {
			t.Fatalf("port 25 accepted the recipient %s (first error: %v)", to, res.firstErr())
		}
	}

	// The refusal of an unknown recipient at the inbound domain is read from
	// the mail log into the journal; the refusal to relay for a stranger's
	// domain is not the relay's mail and is not.
	rows = journalHas(fmt.Sprintf("%s|someone@stranger.e2e.test|nobody@%s||reject|postfix", inboundDomain, inboundDomain))
	if n := strings.Count(rows, "\n") + 1; n != 2 {
		t.Fatalf("the journal holds %d rows, want the relayed message and one refusal:\n%s", n, rows)
	}
}
