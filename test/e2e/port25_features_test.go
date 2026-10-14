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
	if err := res.refusedAt("RCPT", 550, "5.1.1"); err != nil {
		t.Fatalf("port 25, a report address no domain uses: %v", err)
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

	// The filter lists exist as files from the first start with the relay on,
	// readable by a filter that is another user in another container. Nothing
	// is on them until someone adds an entry.
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

	// The inbound journal milter saw it go through, and the Inbound log shows it.
	// Beside the maps, the key the filter signs each message with: there from
	// the first start with the relay on, whether or not a filter is attached
	// yet, one line of 64 hex characters, readable by the filter.
	if key, err := h.execIn("selfpost", "stat", "-c", "%a %s", "/data/antispam/filter.key"); err != nil || strings.TrimSpace(key) != "644 65" {
		t.Fatalf("/data/antispam/filter.key: %q, %v; want mode 644 and 65 bytes", key, err)
	}

	// No filter is attached on the stand: the row is an accept by nobody, so its
	// reason is the engine alone.
	logRows := func(want string) []string {
		t.Helper()
		var rows []string
		var body string
		for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(time.Second) {
			resp, b, err := sc.panel.get("/inbound/log")
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("GET /inbound/log: status %v, %v\n%.800s", resp, err, b)
			}
			body = b
			rows = rows[:0]
			for _, row := range strings.Split(body, "<tr><td")[1:] {
				// The row's own end is the last one: the sheet in its
				// decision cell holds a table of its own.
				rows = append(rows, row[:strings.LastIndex(row, "</tr>")])
			}
			if strings.Contains(body, want) {
				return rows
			}
			if time.Now().After(deadline) {
				t.Fatalf("no row with %q in the Inbound log:\n%.3000s\n%s", want, body, mailLogTail(h, "postfix/"))
			}
		}
	}
	// rowWith finds the one row that holds all of wants.
	rowWith := func(rows []string, wants ...string) string {
		t.Helper()
		for _, row := range rows {
			ok := true
			for _, w := range wants {
				ok = ok && strings.Contains(row, w)
			}
			if ok {
				return row
			}
		}
		t.Fatalf("no row of the Inbound log holds all of %q in\n%s", wants, strings.Join(rows, "\n"))
		return ""
	}

	rows := logRows("e2e " + token)
	rowWith(rows,
		"someone@<wbr>stranger.e2e.test", "user@<wbr>"+inboundDomain, "e2e "+token,
		`class="tag is-success is-light" popovertarget="why-`, `<th>Decided by</th><td>selfpost</td>`,
		// the way to correct it, which only the global role is given
		`href="/inbound/filter-lists?list=deny&amp;kind=address&amp;value=someone%40stranger.e2e.test">Deny sender</a>`)
	// A report mailed to the server's own name earlier in the run came through
	// the same listener and is not the relay's mail.
	if len(rows) != 1 {
		t.Fatalf("the Inbound log holds %d rows, want the relayed message alone:\n%s", len(rows), strings.Join(rows, "\n"))
	}

	for _, c := range []struct {
		to   string
		code int
		text string
	}{
		{"nobody@" + inboundDomain, 550, "User unknown in relay recipient table"},
		{"someone@elsewhere.e2e.test", 554, "Relay access denied"},
	} {
		res := deliverPort25("someone@stranger.e2e.test", c.to, message(c.to, uniqueToken("refused")))
		if err := res.refusedAt("RCPT", c.code, c.text); err != nil {
			t.Fatalf("port 25, the recipient %s: %v", c.to, err)
		}
	}

	// The refusal of an unknown recipient at the inbound domain is read from
	// the mail log into the journal; the refusal to relay for a stranger's
	// domain is not the relay's mail and is not.
	rows = logRows("nobody@<wbr>" + inboundDomain)
	refused := rowWith(rows,
		"someone@<wbr>stranger.e2e.test", "nobody@<wbr>"+inboundDomain, `<td class="sp-clip sp-muted">—</td>`,
		`class="tag is-danger is-light" popovertarget="why-`, `<th>Decided by</th><td>postfix</td>`,
		`<th>Reply</th><td class="sp-mono sp-small">550 5.1.1 `)
	// A message Postfix itself refused offers no way into the lists.
	if strings.Contains(refused, "sender</a>") {
		t.Fatalf("the refused row offers a way into the filter lists:\n%s", refused)
	}
	if len(rows) != 2 {
		t.Fatalf("the Inbound log holds %d rows, want the relayed message and one refusal:\n%s", len(rows), strings.Join(rows, "\n"))
	}

	// The filter lists: an entry added in the panel is in the map file the
	// filter reads, and one removed is gone from it. Nothing reads the file on
	// this stand (no filter is attached), so the file is the whole effect.
	const spammer = "spammer.e2e.test"
	denyDomain := func() string {
		t.Helper()
		out, err := h.execIn("selfpost", "cat", "/data/antispam/deny_domain.map")
		if err != nil {
			t.Fatalf("read deny_domain.map: %v\n%s", err, out)
		}
		return out
	}
	resp, body, err = sc.panel.postForm("/inbound/filter-lists", url.Values{
		"list_type": {"deny"}, "entry_type": {"domain"}, "value": {"  @" + strings.ToUpper(spammer)}, "note": {"e2e"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/inbound/filter-lists" || !strings.Contains(body, "Entry added.") {
		t.Fatalf("add a filter list entry: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	if got := denyDomain(); !strings.Contains(got, "\n"+spammer+"\n") {
		t.Fatalf("deny_domain.map after adding %s:\n%s", spammer, got)
	}
	if got, err := h.execIn("selfpost", "sh", "-c", `cd /data/antispam && { grep -vc "^#" *.map; true; }`); err != nil ||
		strings.Count(got, ":0") != 5 || !strings.Contains(got, "deny_domain.map:1") {
		t.Fatalf("the entry is in other files than deny_domain.map (%v):\n%s", err, got)
	}
	// The same entry again is refused on the page, and the file still holds it once.
	resp, body, err = sc.panel.postForm("/inbound/filter-lists", url.Values{
		"list_type": {"deny"}, "entry_type": {"domain"}, "value": {spammer}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 422 || !strings.Contains(body, "That entry is already on the list.") {
		t.Fatalf("add the same entry again: status %d:\n%.800s", resp.StatusCode, body)
	}
	if got := denyDomain(); strings.Count(got, spammer) != 1 {
		t.Fatalf("deny_domain.map after a duplicate:\n%s", got)
	}

	resp, body, err = sc.panel.get("/inbound/filter-lists")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET /inbound/filter-lists: status %v, %v", resp, err)
	}
	m = regexp.MustCompile(`action="(/inbound/filter-lists/\d+/delete)"`).FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, spammer) {
		t.Fatalf("the list does not show %s with a Remove:\n%.2000s", spammer, body)
	}
	resp, body, err = sc.panel.postForm(m[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/inbound/filter-lists" || !strings.Contains(body, "Entry removed.") {
		t.Fatalf("remove the entry: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	if got := denyDomain(); strings.Contains(got, spammer) {
		t.Fatalf("deny_domain.map after removing %s:\n%s", spammer, got)
	}
}
