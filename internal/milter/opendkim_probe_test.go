package milter

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-milter"
)

// TestPortAgainstOpenDKIM checks the port where it matters, in OpenDKIM
// itself: for every From: line, the domain OpenDKIM signs the message for
// must be the one the port predicts - the port's address, matched against
// the SigningTable as dkimf_apply_signtable() matches it, cut to 256 bytes
// (MAXADDRESS) - or both must say no signature. It runs where
// OPENDKIM_PROBE_SOCKET names a running OpenDKIM's socket and
// OPENDKIM_PROBE_TABLE its refile SigningTable of "*@<domain>" lines:
// test/upstream/opendkim-probe.sh starts one from the image and runs this
// (audit 2026-10-12, stage 2c).
func TestPortAgainstOpenDKIM(t *testing.T) {
	sock, table := os.Getenv("OPENDKIM_PROBE_SOCKET"), os.Getenv("OPENDKIM_PROBE_TABLE")
	if sock == "" || table == "" {
		t.Skip("OPENDKIM_PROBE_SOCKET and OPENDKIM_PROBE_TABLE not set")
	}
	signed := map[string]bool{}
	f, err := os.Open(table)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) > 0 && strings.HasPrefix(fields[0], "*@") {
			signed[strings.ToLower(fields[0][2:])] = true
		}
	}
	f.Close()

	const own, other, server = "sender.e2e.test", "other.e2e.test", "mail.e2e.test"
	lines := probeLines(t, own, other, server)

	agree, signedCount := 0, 0
	for i, line := range lines {
		want := predictSigner(line, signed)
		got, err := opendkimSigns(sock, line)
		if err != nil {
			t.Fatalf("From: %q: %v", line, err)
		}
		if i >= len(lines)-2 && got != other {
			// The padded lines: cut at 256 bytes they end in "@<other>",
			// and OpenDKIM signs them with that domain's key - the reason
			// the sender check refuses an address over 256 bytes.
			t.Errorf("a padded line of %d bytes: OpenDKIM signs for %q, not %q", len(line), got, other)
		}
		if got != "" {
			signedCount++
		}
		if got != want {
			t.Errorf("From: %q: OpenDKIM signs for %q, the port predicts %q", line, got, want)
			continue
		}
		agree++
	}
	t.Logf("%d From: lines, OpenDKIM and the port agree on %d; %d signed", len(lines), agree, signedCount)
}

// predictSigner is the domain OpenDKIM signs a message with this From: line
// for, by the port: none when it reads no address; otherwise the domain of
// the SigningTable whose "^.*@<domain>$" (case-insensitive) matches
// "user@domain" as snprintf() leaves it in MAXADDRESS + 1 bytes, the domain
// first cut to DKIM_MAXHOSTNAMELEN by strlcpy() and lower-cased.
func predictSigner(line string, signed map[string]bool) string {
	r := dkimMailParse(line)
	if r.err != mailparseOK || !r.hasUser || !r.hasDomain || r.user == "" || r.domain == "" {
		return ""
	}
	domain := r.domain
	if len(domain) > 256 {
		domain = domain[:256]
	}
	addr := r.user + "@" + asciiLower(domain)
	if len(addr) > 256 {
		addr = addr[:256]
	}
	d := asciiLower(addr[strings.LastIndexByte(addr, '@')+1:])
	if signed[d] {
		return d
	}
	return ""
}

// opendkimSigns plays one message to OpenDKIM as Postfix would for a client
// it trusts (127.0.0.1, its InternalHosts by default) and returns the d= of
// the signature it adds, lower-cased, or "".
func opendkimSigns(sock, from string) (string, error) {
	cl := milter.NewClientWithOptions("unix", sock, milter.ClientOptions{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		ActionMask:   milter.OptAddHeader | milter.OptChangeHeader,
	})
	defer cl.Close()
	s, err := cl.Session()
	if err != nil {
		return "", err
	}
	defer s.Close()
	// A step OpenDKIM answers with anything but "continue" ends the message
	// for it - an accept where it has nothing to do (libmilter, engine.c) -
	// and it answers nothing after; no signature then.
	step := func(act *milter.Action, err error) (bool, error) {
		if err != nil {
			return false, err
		}
		return act.Code == milter.ActContinue, nil
	}
	if ok, err := step(s.Conn("localhost", milter.FamilyInet, 40000, "127.0.0.1")); !ok {
		return "", err
	}
	if ok, err := step(s.Mail("<probe@sender.e2e.test>", nil)); !ok {
		return "", err
	}
	if ok, err := step(s.Rcpt("<user@elsewhere.example>", nil)); !ok {
		return "", err
	}
	for _, h := range [][2]string{{"From", from}, {"To", "user@elsewhere.example"}, {"Subject", "probe"}, {"Date", "Thu, 15 Oct 2026 03:00:00 +0000"}} {
		if ok, err := step(s.HeaderField(h[0], h[1])); !ok {
			return "", err
		}
	}
	if ok, err := step(s.HeaderEnd()); !ok {
		return "", err
	}
	// No body chunk: OpenDKIM signs an empty body as well.
	mods, _, err := s.End()
	if err != nil {
		return "", fmt.Errorf("end: %w", err)
	}
	for _, m := range mods {
		if !strings.EqualFold(m.HeaderName, "DKIM-Signature") {
			continue
		}
		for _, tag := range strings.Split(m.HeaderValue, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(tag), "=")
			if ok && strings.TrimSpace(k) == "d" {
				return strings.ToLower(strings.TrimSpace(v)), nil
			}
		}
	}
	return "", nil
}

// probeLines are the shared files' lines and several hundred composed from
// pieces: display names plain, quoted, escaped and encoded, comments nested
// and left open, groups, lists, brackets, source routes, domain literals and
// quoted local parts - with the other sender's name, and the server's, in
// each place - and the line padded to be cut at 256 bytes.
func probeLines(t *testing.T, own, other, server string) []string {
	var lines []string
	for _, file := range []string{"from-lines-passed.txt", "from-lines-other.txt"} {
		lines = append(lines, fromLines(t, file, own, other)...)
	}
	names := []string{"", "Shop ", `"Shop, Inc." `, `"x@` + other + `" `, "x@" + other + " ", `"a\"b <x@` + other + `>" `,
		"=?utf-8?q?x=40" + other + "?= ", "(x@" + other + ") ", "((x@" + other + ")) ", "(open ", `"open `, "[x@" + other + "] ",
		"group: ", other + ": "}
	addrs := []string{
		"a@" + own, "<a@" + own + ">", "<a@" + other + ">", "a@" + other, "<a@" + server + ">", "a@" + strings.ToUpper(own),
		`<"x@` + other + `"@` + own + `>`, `"x@` + own + `"@` + other, "<@" + other + ":a@" + own + ">", "<@" + own + ":a@" + other + ">",
		"a@[192.0.2.1]", "<a@" + own + ">, b@" + other, "a@" + own + ", <b@" + other + ">", "a(c)@" + own, "a@" + own + "(x@" + other + ")",
		"a@ " + own, "a @" + own, "<a@" + own, "a@" + own + ";", "@" + own, "<>", "a", `"a b"@` + own, `a\@` + other + "@" + own,
		"a@" + own + " x@" + other,
	}
	for _, n := range names {
		for _, a := range addrs {
			lines = append(lines, n+a)
		}
	}
	// Cut at 256 bytes, the quoted local part ends in "@<other>".
	pad := strings.Repeat("a", 256-len("@"+other)-1)
	lines = append(lines, `"`+pad+`@`+other+`x"@`+own, `<"`+pad+`@`+other+`x"@`+own+`>`)
	return lines
}
