package e2e

import (
	"fmt"
	"regexp"
	"strings"
)

// optionCheck asserts one smtpd `-o` option on the generated smtp/inet
// stanza. When want is non-empty the option's value must contain it; when
// wantExact is set the value must equal it exactly (used for options — like
// smtpd_milters — where an unexpectedly non-empty value is itself the bug).
type optionCheck struct {
	param     string
	contains  string
	wantExact bool
}

// port25Case is one combination of the optional port-25 flags, with the
// options its generated smtp/inet stanza must carry.
type port25Case struct {
	name  string
	env   []string
	check []optionCheck
}

var port25Cases = []port25Case{
	{
		name: "inbound relay only",
		env: []string{
			"INBOUND_RELAY_ENABLE=true",
			"DMARC_REPORTS_ENABLE=false",
		},
		check: []optionCheck{
			{param: "smtpd_sasl_auth_enable", contains: "no", wantExact: true},
			{param: "smtpd_relay_restrictions", contains: "reject_unauth_destination", wantExact: true},
			{param: "smtpd_recipient_restrictions", contains: "reject_unauth_destination,reject_unlisted_recipient", wantExact: true},
			// No antispam milter configured: the submission chain (strict
			// OpenDKIM + journal-milter) must not reach inbound mail.
			{param: "smtpd_milters", contains: "", wantExact: true},
		},
	},
	{
		name: "dmarc ingest only",
		env: []string{
			"INBOUND_RELAY_ENABLE=false",
			"DMARC_REPORTS_ENABLE=true",
		},
		check: []optionCheck{
			{param: "smtpd_recipient_restrictions", contains: "check_recipient_access texthash:/data/postfix/dmarc_recipients,reject", wantExact: true},
			{param: "smtpd_milters", contains: "", wantExact: true},
		},
	},
	{
		name: "inbound relay and dmarc ingest",
		env: []string{
			"INBOUND_RELAY_ENABLE=true",
			"DMARC_REPORTS_ENABLE=true",
		},
		check: []optionCheck{
			{param: "smtpd_recipient_restrictions", contains: "check_recipient_access texthash:/data/postfix/dmarc_recipients,reject_unauth_destination,reject_unlisted_recipient", wantExact: true},
			{param: "smtpd_milters", contains: "", wantExact: true},
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
		check: []optionCheck{
			// Exact equality also covers the absence of OpenDKIM/journal-milter
			// sockets — anything else there would fail the comparison.
			{param: "smtpd_milters", contains: "{inet:antispam:11332,default_action=accept}", wantExact: true},
		},
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
		opts := parseSMTPInetOptions(stanza)
		for _, c := range tc.check {
			got, ok := opts[c.param]
			switch {
			case !ok:
				return fmt.Errorf("%s: smtp/inet has no -o %s (options: %+v)", tc.name, c.param, opts)
			case c.wantExact && got != c.contains:
				return fmt.Errorf("%s: -o %s = %q, want exactly %q", tc.name, c.param, got, c.contains)
			case !c.wantExact && !strings.Contains(got, c.contains):
				return fmt.Errorf("%s: -o %s = %q, want it to contain %q", tc.name, c.param, got, c.contains)
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

// smtpInetStanza returns the smtp/inet service line and its -o options. The
// match requires the second whitespace-separated field to be "inet" — the
// same test master_cf_set_smtp_inet_o uses (build/postfix-config.sh) —
// because master.cf also has a "smtp unix ... smtp" delivery-agent service
// (the outbound SMTP client) whose line starts with the same "smtp" prefix
// and would otherwise be matched instead.
func smtpInetStanza(master string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(master, "\n") {
		switch {
		case in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
		case in:
			return b.String()
		case isSMTPInetServiceLine(line):
			in = true
		default:
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// isSMTPInetServiceLine reports whether line is master.cf's "smtp inet ..."
// service definition (as opposed to "smtp unix ..." or anything else).
func isSMTPInetServiceLine(line string) bool {
	fields := strings.Fields(line)
	return len(fields) >= 2 && fields[0] == "smtp" && fields[1] == "inet"
}

// optionBoundary matches the start of one "-o param=" assignment in the
// option stream (see parseSMTPInetOptions).
var optionBoundary = regexp.MustCompile(`-o\s+([A-Za-z0-9_]+)=`)

// parseSMTPInetOptions reconstructs each logical "-o param=value" assignment
// from a stanza, joining Postfix's own master.cf line-continuations first.
//
// postconf wraps long master.cf lines at a fixed column using master.cf's
// whitespace-continuation convention — any line-writing postconf call (here,
// the unconditional `postconf -F "*/*/chroot=n"` later in
// build/postfix-config.sh) rewraps the *whole* file, including options this
// script wrote as one raw line via master_cf_set_smtp_inet_o. The wrap does
// not respect option boundaries: it can split an option's own value (e.g.
// smtpd_recipient_restrictions's two space-separated tokens) across lines,
// and it re-flows several originally separate "-o " lines into one paragraph.
// This is valid, functional master.cf syntax (`postfix check` accepts it) —
// so the fix is on the reading side: join every stanza line after the header
// into one continuous token stream before splitting it back into options.
func parseSMTPInetOptions(stanza string) map[string]string {
	lines := strings.Split(strings.TrimRight(stanza, "\n"), "\n")
	var stream strings.Builder
	for _, line := range lines[1:] { // skip the "smtp inet ..." header
		if stream.Len() > 0 {
			stream.WriteByte(' ')
		}
		stream.WriteString(strings.TrimSpace(line))
	}
	text := stream.String()

	opts := make(map[string]string)
	matches := optionBoundary.FindAllStringSubmatchIndex(text, -1)
	for i, m := range matches {
		param := text[m[2]:m[3]]
		valueStart := m[1]
		valueEnd := len(text)
		if i+1 < len(matches) {
			valueEnd = matches[i+1][0]
		}
		opts[param] = strings.TrimSpace(text[valueStart:valueEnd])
	}
	return opts
}
