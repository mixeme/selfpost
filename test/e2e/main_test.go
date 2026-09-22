// Package e2e is the hermetic container e2e gate: it drives the shipped
// deploy/docker-compose.yml (plus a test-only override) exactly as an
// administrator and their applications would, so the class of failure unit
// tests cannot see — broken container wiring — has one place to be caught
// before an image is published.
//
// It is a separate module on purpose (see ../../docs/development.md):
// `go test ./...` in the main module never pulls this in, and its test-only
// dependencies (DKIM verification) never enter the shipped binaries' build
// graph.
package e2e

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

var statusCellPattern = regexp.MustCompile(`<td>(queued|sent|deferred|bounced|rejected)</td>`)

// h is the single shared stand for the whole ordered scenario in TestE2E.
// TestHostnameGate does not use it — it spins its own disposable container.
var h *stack

func TestMain(m *testing.M) {
	s, err := newStack()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: build stack:", err)
		os.Exit(1)
	}
	h = s

	if err := prepareStage(s); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: prepare stage:", err)
		os.Exit(1)
	}
	if err := s.build(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: build:", err)
		os.Exit(1)
	}
	if err := s.up(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: up:", err)
		s.down()
		os.Exit(1)
	}

	code := m.Run()

	if code != 0 {
		for _, svc := range []string{"selfpost", "coredns", "sink"} {
			fmt.Fprintf(os.Stderr, "\n==== logs: %s ====\n%s\n", svc, s.logs(svc))
		}
	}
	// Panel/postfix-owned files under the /data bind mount outlive the
	// container; reclaim ownership while selfpost is still up so a later
	// prepareStage RemoveAll (or a local re-run) is not stuck on EACCES.
	s.reclaimData()
	s.down()
	os.Exit(code)
}

// senderDomain and recipient are fixed for the whole run: one sending domain
// registered in the panel, and the sink-MX as the single recipient every
// positive send targets.
const (
	senderDomain = "sender.e2e.test"
	recipient    = "rcpt@sink.e2e.test"
)

// scenario carries state forward between the ordered subtests of TestE2E:
// each later step needs something an earlier one produced (the domain id, the
// DKIM record, the application credentials, an observed client IP).
type scenario struct {
	panel *panelClient

	domainID        string
	dkimName        string
	dkimValue       string
	dkimZoneRelName string // dkimName without the trailing ".e2e.test."
	appLogin        string
	appPassword     string
	l2AppLogin      string
	l2AppPassword   string
	clientIP        string
	zoneRecords     []txtRecord
}

func TestE2E(t *testing.T) {
	sc := &scenario{}

	// Ordered scenario: each step needs state from earlier ones. A failed
	// subtest must stop the rest — otherwise sc.panel stays nil and the next
	// step panics, masking the real failure (as seen on the v1.0.0 release CI).
	run := func(name string, fn func(*testing.T)) {
		if t.Failed() {
			return
		}
		t.Run(name, fn)
	}

	run("startup_processes_running", func(t *testing.T) {
		if err := checkSupervisorProcesses(h); err != nil {
			t.Fatal(err)
		}
		if err := waitForPanelReady(); err != nil {
			t.Fatal(err)
		}
	})

	run("setup_and_login", func(t *testing.T) {
		token, err := readSetupToken(h)
		if err != nil {
			t.Fatalf("read setup token: %v", err)
		}
		p, err := newPanelClient()
		if err != nil {
			t.Fatal(err)
		}
		if err := p.setup(token, "e2eadmin", "correct-horse-battery-staple"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := p.login("e2eadmin", "correct-horse-battery-staple"); err != nil {
			t.Fatalf("login: %v", err)
		}
		sc.panel = p
	})

	run("add_domain_and_publish_dkim", func(t *testing.T) {
		id, err := sc.panel.addDomain(senderDomain)
		if err != nil {
			t.Fatalf("add domain: %v", err)
		}
		sc.domainID = id

		name, value, err := sc.panel.dkimRecord(id)
		if err != nil {
			t.Fatalf("read dkim record: %v", err)
		}
		sc.dkimName, sc.dkimValue = name, value

		relName, ok := trimZoneSuffix(name)
		if !ok {
			t.Fatalf("dkim record name %q is not under e2e.test", name)
		}
		sc.dkimZoneRelName = relName

		records, err := publishTXT(h.stageDir, sc.zoneRecords, txtRecord{name: relName, value: value})
		if err != nil {
			t.Fatalf("publish dkim record to fake zone: %v", err)
		}
		sc.zoneRecords = records
	})

	run("add_application", func(t *testing.T) {
		login, password, err := sc.panel.addApplication(sc.domainID, "app1", "wildcard", "")
		if err != nil {
			t.Fatalf("add application: %v", err)
		}
		sc.appLogin, sc.appPassword = login, password
	})

	run("send_verify_dkim_and_status", func(t *testing.T) {
		token := uniqueToken("positive")
		res := attemptSend(sendAttempt{
			authLogin: sc.appLogin, authPassword: sc.appPassword,
			from: "alerts@" + senderDomain, to: recipient,
			subject: "e2e " + token, body: "hello from selfpost e2e",
		})
		if !res.ok() {
			t.Fatalf("positive-path send failed: dial=%v auth=%v mail=%v rcpt=%v data=%v",
				res.dialErr, res.authErr, res.mailErr, res.rcptErr, res.dataErr)
		}

		raw, err := findSinkMessage(h.stageDir, token, 30*time.Second)
		if err != nil {
			t.Fatalf("message never reached the sink: %v", err)
		}
		if err := verifyDKIM(raw, senderDomain); err != nil {
			t.Fatalf("DKIM did not verify against the record the panel published: %v", err)
		}

		if err := waitFor("send-log row to reach status=sent", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
			rows, err := sc.panel.sendLogRows(senderDomain, "")
			if err != nil {
				return false, err
			}
			if containsCell(rows, "sent") {
				return true, nil
			}
			return false, fmt.Errorf("send log not yet at status=sent (still %s)", firstStatusCell(rows))
		}); err != nil {
			t.Fatal(err)
		}
	})

	run("negative_level2_ratelimit_via_panel", func(t *testing.T) {
		testLevel2RateLimit(t, sc)
	})
	run("negative_sender_login_mismatch", func(t *testing.T) {
		testSenderLoginMismatch(t, sc)
	})
	run("negative_no_auth_rejected", func(t *testing.T) {
		testNoAuthRejected(t, sc)
	})
	run("negative_foreign_relay_rejected", func(t *testing.T) {
		testForeignRelayRejected(t, sc)
	})
	run("negative_journal_milter_fail_open", func(t *testing.T) {
		testJournalMilterFailOpen(t, sc)
	})
	run("session_survives_restart", func(t *testing.T) {
		testSessionSurvivesRestart(t, sc)
	})
	run("negative_level1_ratelimit", func(t *testing.T) {
		testLevel1RateLimit(t, sc)
	})
}

// trimZoneSuffix strips the "e2e.test" suffix a fully-qualified DKIM record
// name carries, returning the name relative to the zone db.zone's $ORIGIN
// expects (see dns_zone.go).
func trimZoneSuffix(fqdn string) (string, bool) {
	const suffix = ".e2e.test"
	if len(fqdn) > len(suffix) && fqdn[len(fqdn)-len(suffix):] == suffix {
		return fqdn[:len(fqdn)-len(suffix)], true
	}
	return "", false
}

var tokenCounter int

// uniqueToken returns a short, human-readable, monotonically distinct marker
// safe to embed in a Subject header and grep for in the sink's dump
// directory.
func uniqueToken(label string) string {
	tokenCounter++
	return fmt.Sprintf("%s-%d-%d", label, time.Now().UnixNano(), tokenCounter)
}

func containsCell(html, needle string) bool {
	return strings.Contains(html, "<td>"+needle+"</td>")
}

func firstStatusCell(html string) string {
	m := statusCellPattern.FindStringSubmatch(html)
	if m == nil {
		return "(no rows yet)"
	}
	return m[1]
}
