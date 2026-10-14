package milter

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mailparseCases are lines whose result is written down here, read off
// dkim-mailparse.c by hand and confirmed against it by
// TestMailParseMatchesOpenDKIM.
var mailparseCases = []struct {
	line         string
	user, domain string // "-" for absent
	err          int
}{
	{"user@example.com", "user", "example.com", 0},
	{" Name <user@example.com>", "user", "example.com", 0},
	{`"Some One" <user@example.com>`, "user", "example.com", 0},
	{"user@example.com (Some One)", "user", "example.com", 0},
	{"Some One <user@ex(comment)ample.com>", "user", "example.com", 0},
	// An '@' before the first ',' decides: the address is read from the start.
	{"ceo@other.com <me@mine.com>", "me", "mine.com", 0},
	{"me@mine.com, you@yours.com", "me", "mine.com", 0},
	// Quoted local parts keep their quotes; an '@' inside them is no '@'.
	{`"x@other.com"@mine.com`, `"x@other.com"`, "mine.com", 0},
	{`<"a b"@mine.com>`, `"a b"`, "mine.com", 0},
	// A source route is dropped.
	{"<@relay.example,@r2.example:u@mine.com>", "u", "mine.com", 0},
	// Group syntax: the name is skipped.
	{"group: u@mine.com;", "u", "mine.com", 0},
	// No local part: the domain is taken for it, so OpenDKIM has both.
	{"@mine.com", "mine.com", "mine.com", 0},
	// Whitespace between atoms of the default form becomes one space.
	{"a b@mine.com", "a b", "mine.com", 0},
	// Unbalanced.
	{"Name (comment <u@mine.com>", "-", "-", mailparsePUnbalanced},
	{`"open <u@mine.com>`, "-", "-", mailparseQUnbalanced},
	{"u@[1.2.3.4", "-", "-", mailparseSUnbalanced},
	// Encoded words are not decoded.
	{"=?utf-8?q?boss=40other.com?= <u@mine.com>", "u", "mine.com", 0},
	// No address at all.
	{"Undisclosed recipients:;", "-", "-", 0},
	{"", "-", "-", 0},
}

func parsedParts(r mailParsed) (string, string) {
	u, d := "-", "-"
	if r.hasUser {
		u = r.user
	}
	if r.hasDomain {
		d = r.domain
	}
	return u, d
}

func TestMailParseCases(t *testing.T) {
	for _, c := range mailparseCases {
		r := dkimMailParse(c.line)
		u, d := parsedParts(r)
		if r.err != c.err || (c.err == 0 && (u != c.user || d != c.domain)) {
			t.Errorf("%q: user %q domain %q err %d; want %q %q %d", c.line, u, d, r.err, c.user, c.domain, c.err)
		}
	}
}

func TestMoreAfter(t *testing.T) {
	for line, want := range map[string]bool{
		"user@example.com":               false,
		"Name <user@example.com>":        false,
		"Name <user@example.com> (note)": false,
		"group: u@mine.com;":             false,
		"me@mine.com, you@yours.com":     true,
		"Name <a@x>, b@y":                true,
		"Name <a@x> trailing":            true,
	} {
		r := dkimMailParse(line)
		if got := moreAfter(line, r.rest); got != want {
			t.Errorf("%q: moreAfter %v, want %v", line, got, want)
		}
	}
}

// mailparseCorpus is the cases above and n random lines over an alphabet
// weighted toward what the parser treats specially.
func mailparseCorpus(n int) []string {
	lines := []string{}
	for _, c := range mailparseCases {
		lines = append(lines, c.line)
	}
	alphabet := []string{"<", ">", "@", ",", ";", ":", "\\", `"`, "/", "[", "]", "?", "=", "(", ")",
		" ", "\t", "\n", "\r", ".", "a", "b", "x", "mine.com", "other.com", "\xc3\xa9", "\xff", "=?utf-8?q?", "?="}
	rng := rand.New(rand.NewSource(20261015))
	for i := 0; i < n; i++ {
		var b strings.Builder
		for j := rng.Intn(24); j >= 0; j-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		lines = append(lines, b.String())
	}
	return lines
}

// TestMailParseMatchesOpenDKIM feeds the corpus to OpenDKIM's own C
// dkim_mail_parse() through testdata/mailparse_harness.c, built against the
// opendkim source the image runs, and wants the port's answer for every line.
// It runs where MAILPARSE_C names the built harness.
func TestMailParseMatchesOpenDKIM(t *testing.T) {
	bin := os.Getenv("MAILPARSE_C")
	if bin == "" {
		t.Skip("MAILPARSE_C not set (see testdata/mailparse_harness.c)")
	}
	lines := mailparseCorpus(200000)
	var in strings.Builder
	for _, l := range lines {
		in.WriteString(hex.EncodeToString([]byte(l)))
		in.WriteByte('\n')
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := func(present bool, s string) string {
		if !present {
			return "-"
		}
		return "=" + hex.EncodeToString([]byte(s))
	}
	diffs, failed := 0, 0
	for i := 0; sc.Scan(); i++ {
		r := dkimMailParse(lines[i])
		got := fmt.Sprintf("%d %s %s", r.err, enc(r.hasUser, r.user), enc(r.hasDomain, r.domain))
		want := sc.Text()
		if r.err != 0 {
			// On an error the C returns before it writes the result's end, so
			// what it prints of user and domain runs on into the unread
			// input; OpenDKIM uses neither then (mlfi_eoh(): status != 0 is
			// BADFORMAT, no signature). The error itself must agree.
			failed++
			got, want = fmt.Sprint(r.err), strings.Fields(want)[0]
		}
		if got != want {
			diffs++
			if diffs <= 20 {
				t.Errorf("line %q: port %s, OpenDKIM %s", lines[i], got, want)
			}
		}
	}
	if diffs > 0 {
		t.Fatalf("%d of %d lines differ", diffs, len(lines))
	}
	t.Logf("%d lines, the port and OpenDKIM agree on every one (%d of them errors, compared by code)", len(lines), failed)
}
