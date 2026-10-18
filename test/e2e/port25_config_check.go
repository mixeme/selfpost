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
	// main are main.cf parameters the combination must end up with, read with
	// postconf -h.
	main []optionCheck
	// ingest is set when the combination must carry the DMARC report delivery
	// service, with everything the command it runs depends on.
	ingest bool
}

// ingestService is the dmarc-ingest service as Postfix must read it back.
// flags=, user= and argv= are arguments of pipe(8); set as -o parameters they
// are dropped, and a bare "pipe" dies on every report with "fatal: missing
// user= command-line attribute" - the state of 1.7.0 through 1.9.5 (date not
// recorded; Postfix 3.10.13, src/pipe/pipe.c, get_service_attr()).
const ingestService = "dmarc-ingest unix - n n - 1 pipe flags=Rq user=panel argv=/usr/local/bin/dmarc-ingest ${recipient}"

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
			// The log tailer tells this listener's refusals from the
			// submission ports' by the name they are logged under.
			{param: "syslog_name", contains: "postfix/inbound", wantExact: true},
			// The SMTP-smuggling defence. Postfix 3.10.13 has it by default
			// (src/global/mail_params.h, DEF_SMTPD_FORBID_BARE_LF "normalize");
			// set explicitly since 2.6.1, when the image ran Postfix 3.7.11,
			// whose default was "no" (docs/security.md § Mail path), and kept so the
			// listener does not depend on the default. testInboundBareNewline
			// drives it.
			{param: "smtpd_forbid_bare_newline", contains: "normalize", wantExact: true},
			// No antispam milter configured: the inbound journal milter
			// alone. The submission chain (the strict sender check and
			// signer + journal-milter) must not reach inbound mail.
			{param: "smtpd_milters", contains: "{unix:/run/selfpost/inbound-journal.sock,default_action=accept}", wantExact: true},
		},
	},
	{
		name: "dmarc ingest only",
		env: []string{
			"INBOUND_RELAY_ENABLE=false",
			"DMARC_REPORTS_ENABLE=true",
		},
		check: []optionCheck{
			{param: "smtpd_recipient_restrictions", contains: "check_recipient_access,texthash:/data/postfix/dmarc_recipients,reject", wantExact: true},
			{param: "smtpd_milters", contains: "", wantExact: true},
		},
		ingest: true,
	},
	{
		name: "inbound relay and dmarc ingest",
		env: []string{
			"INBOUND_RELAY_ENABLE=true",
			"DMARC_REPORTS_ENABLE=true",
		},
		check: []optionCheck{
			{param: "smtpd_recipient_restrictions", contains: "check_recipient_access,texthash:/data/postfix/dmarc_recipients,reject_unauth_destination,reject_unlisted_recipient", wantExact: true},
			{param: "smtpd_milters", contains: "{unix:/run/selfpost/inbound-journal.sock,default_action=accept}", wantExact: true},
		},
		// Both on: the report addresses sit on a relay domain, and the relay's
		// unlisted-recipient check runs after the restriction list. Unless their
		// table is a recipient table too, every report is refused as "User
		// unknown in relay recipient table".
		main: []optionCheck{
			{param: "relay_recipient_maps", contains: "texthash:/data/postfix/relay_recipients texthash:/data/postfix/dmarc_recipients", wantExact: true},
		},
		ingest: true,
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
			// Exact equality also covers the absence of the submission
			// chain's sockets — anything else there would fail the comparison.
			{param: "smtpd_milters", contains: "{inet:antispam:11332,default_action=accept},{unix:/run/selfpost/inbound-journal.sock,default_action=accept}", wantExact: true},
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
			// What the file says is not yet what Postfix reads: master splits
			// a service line on whitespace, so a value with a space in it is
			// cut at the space and the rest becomes a stray smtpd argument
			// ("fatal: unexpected command-line argument", port 25 dead while
			// `postfix check` stays silent). Ask Postfix itself. (Seen in 1.9.1,
			// date not recorded; Postfix 3.10.13, man/man5/master.5, "-o
			// name=value (short form)": "does not support whitespace
			// around the "=" or in parameter values".)
			seen, err := s.execIn("selfpost", "postconf", "-Ph", "smtp/inet/"+c.param)
			if err != nil {
				return fmt.Errorf("%s: postconf -P smtp/inet/%s: %v\n%s", tc.name, c.param, err, seen)
			}
			if strings.TrimSpace(seen) != got {
				return fmt.Errorf("%s: master.cf has -o %s = %q but Postfix reads %q - the value contains whitespace", tc.name, c.param, got, strings.TrimSpace(seen))
			}
		}
		for _, c := range tc.main {
			got, err := s.execIn("selfpost", "postconf", "-h", c.param)
			if err != nil {
				return fmt.Errorf("%s: postconf -h %s: %v\n%s", tc.name, c.param, err, got)
			}
			if got = strings.TrimSpace(got); got != c.contains {
				return fmt.Errorf("%s: %s = %q, want %q", tc.name, c.param, got, c.contains)
			}
		}
		if err := checkIngestService(s, tc); err != nil {
			return err
		}
	}

	// Back to the stand's own configuration: flags off, no port-25 listener.
	if err := runPostfixConfig(s, []string{"INBOUND_RELAY_ENABLE=false", "DMARC_REPORTS_ENABLE=false"}); err != nil {
		return fmt.Errorf("restore flags-off configuration: %w", err)
	}
	return checkInboundRelayOff(s)
}

// checkIngestService asks Postfix for the dmarc-ingest service and the two
// main.cf parameters its command depends on: one recipient per run, so
// ${recipient} is one argument, and DMARC_REPORTS_ENABLE in the environment
// pipe(8) gives the command - it passes export_environment and nothing else.
// A combination without DMARC ingest must have none of the three left over.
func checkIngestService(s *stack, tc port25Case) error {
	service, _ := s.execIn("selfpost", "postconf", "-M", "dmarc-ingest/unix")
	service = strings.Join(strings.Fields(service), " ")
	limit, _ := s.execIn("selfpost", "postconf", "-h", "dmarc-ingest_destination_recipient_limit")
	limit = strings.TrimSpace(limit)
	environ, err := s.execIn("selfpost", "postconf", "-h", "export_environment")
	if err != nil {
		return fmt.Errorf("%s: postconf -h export_environment: %v\n%s", tc.name, err, environ)
	}
	exported := strings.Contains(" "+strings.TrimSpace(environ)+" ", " DMARC_REPORTS_ENABLE=true ")
	if !tc.ingest {
		if strings.Contains(service, "pipe") || limit == "1" || exported {
			return fmt.Errorf("%s: DMARC ingest is off but left behind: service %q, recipient limit %q, export_environment %q", tc.name, service, limit, environ)
		}
		return nil
	}
	if service != ingestService {
		return fmt.Errorf("%s: Postfix reads the dmarc-ingest service as %q, want %q", tc.name, service, ingestService)
	}
	if limit != "1" {
		return fmt.Errorf("%s: dmarc-ingest_destination_recipient_limit = %q, want 1", tc.name, limit)
	}
	if !exported {
		return fmt.Errorf("%s: export_environment = %q, want DMARC_REPORTS_ENABLE=true in it", tc.name, environ)
	}
	return nil
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
