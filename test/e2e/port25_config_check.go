package e2e

import (
	"fmt"
	"strings"
)

// port25Case is one combination of the optional port-25 flags, with the
// master.cf `-o` lines its generated smtp/inet stanza must carry.
type port25Case struct {
	name string
	env  []string
	want []string
	deny []string
}

var port25Cases = []port25Case{
	{
		name: "inbound relay only",
		env: []string{
			"INBOUND_RELAY_ENABLE=true",
			"DMARC_REPORTS_ENABLE=false",
		},
		want: []string{
			"-o smtpd_sasl_auth_enable=no",
			"-o smtpd_relay_restrictions=reject_unauth_destination",
			"-o smtpd_recipient_restrictions=reject_unauth_destination,reject_unlisted_recipient",
			// No antispam milter configured: the submission chain (strict
			// OpenDKIM + journal-milter) must not reach inbound mail.
			"-o smtpd_milters=",
		},
	},
	{
		name: "dmarc ingest only",
		env: []string{
			"INBOUND_RELAY_ENABLE=false",
			"DMARC_REPORTS_ENABLE=true",
		},
		want: []string{
			"-o smtpd_recipient_restrictions=check_recipient_access texthash:/data/postfix/dmarc_recipients,reject",
			"-o smtpd_milters=",
		},
	},
	{
		name: "inbound relay and dmarc ingest",
		env: []string{
			"INBOUND_RELAY_ENABLE=true",
			"DMARC_REPORTS_ENABLE=true",
		},
		want: []string{
			"-o smtpd_recipient_restrictions=check_recipient_access texthash:/data/postfix/dmarc_recipients,reject_unauth_destination,reject_unlisted_recipient",
			"-o smtpd_milters=",
		},
	},
	{
		name: "inbound relay with antispam milter",
		env: []string{
			"INBOUND_RELAY_ENABLE=true",
			"DMARC_REPORTS_ENABLE=false",
			"INBOUND_ANTISPAM_MILTER=inet:antispam:11332",
			"INBOUND_ANTISPAM_MILTER_ACTION=accept",
		},
		want: []string{
			"-o smtpd_milters={inet:antispam:11332,default_action=accept}",
		},
		deny: []string{"opendkim.sock", "journal.sock"},
	},
}

// checkPort25ConfigCombinations regenerates the Postfix configuration inside
// the running container for every combination of the optional port-25 flags
// and asserts `postfix check` still passes.
//
// This is the regression guard for the 1.9.1 crash-loop: enabling inbound
// relay or DMARC ingest made postfix-config.sh fail at start (postconf -P
// cannot set master.cf -o values containing whitespace), and nothing in the
// suite exercised the enabled path — the stand runs with both flags off, so
// the bug shipped in 1.4.0 and survived three releases.
//
// Only configuration *generation* is exercised: the script is re-run with
// different environment variables and its output inspected. Postfix is not
// reloaded, so the running master keeps the config it started with, and the
// final pass restores the stand's own (flags-off) configuration on disk.
func checkPort25ConfigCombinations(s *stack) error {
	for _, tc := range port25Cases {
		if err := runPostfixConfig(s, tc.env); err != nil {
			return fmt.Errorf("%s: %w", tc.name, err)
		}
		master, err := s.execIn("selfpost", "cat", "/etc/postfix/master.cf")
		if err != nil {
			return fmt.Errorf("%s: read master.cf: %v\n%s", tc.name, err, master)
		}
		stanza := smtpInetStanza(master)
		if stanza == "" {
			return fmt.Errorf("%s: no smtp/inet service in master.cf:\n%s", tc.name, master)
		}
		for _, want := range tc.want {
			if !strings.Contains(stanza, want) {
				return fmt.Errorf("%s: smtp/inet is missing %q:\n%s", tc.name, want, stanza)
			}
		}
		for _, deny := range tc.deny {
			if strings.Contains(stanza, deny) {
				return fmt.Errorf("%s: smtp/inet unexpectedly carries %q:\n%s", tc.name, deny, stanza)
			}
		}
	}

	// Back to the stand's own configuration: flags off, no port-25 listener.
	if err := runPostfixConfig(s, []string{"INBOUND_RELAY_ENABLE=false", "DMARC_REPORTS_ENABLE=false"}); err != nil {
		return fmt.Errorf("restore flags-off configuration: %w", err)
	}
	return checkInboundRelayOff(s)
}

// runPostfixConfig re-runs the config generator with env overrides. The script
// ends with `postfix check`, so a non-zero exit is exactly the start-time
// failure an operator would hit.
func runPostfixConfig(s *stack, env []string) error {
	args := append([]string{"env"}, env...)
	args = append(args, "/usr/local/bin/postfix-config.sh")
	out, err := s.execIn("selfpost", args...)
	if err != nil {
		return fmt.Errorf("postfix-config.sh with %v failed: %v\n%s", env, err, out)
	}
	return nil
}

// smtpInetStanza returns the smtp/inet service line and its -o options.
func smtpInetStanza(master string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(master, "\n") {
		switch {
		case strings.HasPrefix(line, "smtp ") || strings.HasPrefix(line, "smtp\t"):
			in = true
		case in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
		case in:
			return b.String()
		default:
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
