package milter

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-milter"
)

// The milter gets the address as go-milter v0.4.1 hands it: the command's
// first argument, the ESMTP parameters left out, "<>" trimmed (session.go,
// case CodeMail/CodeRcpt). Nothing more is cut: a quoted local part may hold
// a blank or a ">" (assumptions B-G5).
func TestCleanAddress(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a@example.net", "a@example.net"},
		{"", ""},
		{`"a b"@example.net`, `"a b"@example.net`},
		{`"a>b"@example.net`, `"a>b"@example.net`},
		{` a@example.net `, "a@example.net"},
	} {
		if got := cleanAddress(c.in); got != c.want {
			t.Errorf("cleanAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The journal row's recipient has to be the address Postfix later logs in
// to=<…>, or the delivery line matches no row and the sweep closes it as
// bounced: the local part unquoted, then quoted again only when it is not a
// dot-string (Postfix 3.10.13, src/global/tok822_parse.c and
// quote_822_local.c). Composed by hand after those functions.
func TestRecipientAsLogged(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a@example.net", "a@example.net"},
		{"User@Example.NET", "User@Example.NET"}, // case kept
		{`"ab"@example.net`, "ab@example.net"},   // quotes not needed
		{`"a.b"@example.net`, "a.b@example.net"},
		{`"a b"@example.net`, `"a b"@example.net`},
		{`"a\b"@example.net`, "ab@example.net"}, // the backslash only escapes
		{`"a\"b"@example.net`, `"a\"b"@example.net`},
		{`"a..b"@example.net`, `"a..b"@example.net`},
		{`".a"@example.net`, `".a"@example.net`},
		{`"a(b)"@example.net`, `"a(b)"@example.net`},
		{`"почта"@example.net`, "почта@example.net"}, // 8-bit clean
		{"postmaster", "postmaster"},
	} {
		if got := recipientAsLogged(c.in); got != c.want {
			t.Errorf("recipientAsLogged(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Through go-milter's own client, as Postfix sends it: the address and its
// ESMTP parameters as separate strings. A recipient given twice, in another
// case, is one row - cleanup keeps one (assumptions A-31).
func TestRecipientsThroughGoMilter(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rec := &fakeRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Serve(ctx, ln, rec)

	// The library's server speaks milter version 2, so the client asks for no
	// action only version 6 has (go-milter v0.4.1, client.go, negotiate();
	// its client_test.go does the same).
	cl := milter.NewClientWithOptions("tcp", ln.Addr().String(), milter.ClientOptions{
		ActionMask: milter.OptAddHeader | milter.OptChangeHeader,
	})
	defer cl.Close()
	s, err := cl.Session()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	step := func(what string, act *milter.Action, err error) {
		t.Helper()
		if err != nil || act.Code != milter.ActContinue {
			t.Fatalf("%s: %+v, %v", what, act, err)
		}
	}
	act, err := s.Conn("client.example.org", milter.FamilyInet, 40000, "203.0.113.7")
	step("connect", act, err)
	if err := s.Macros(milter.CodeMail, "{auth_authen}", "app1"); err != nil {
		t.Fatal(err)
	}
	act, err = s.Mail("<noreply@example.com>", []string{"SIZE=100", "BODY=8BITMIME"})
	step("mail", act, err)
	for _, r := range []struct {
		rcpt string
		args []string
	}{
		{`<"a b"@example.net>`, []string{"NOTIFY=SUCCESS"}},
		{`<"ab"@example.net>`, nil},
		{`<AB@example.net>`, nil}, // ab@example.net again, in capitals
	} {
		act, err = s.Rcpt(r.rcpt, r.args)
		step("rcpt "+r.rcpt, act, err)
	}
	act, err = s.HeaderField("Subject", "hello")
	step("header", act, err)
	act, err = s.HeaderEnd()
	step("end of headers", act, err)
	if err := s.Macros(milter.CodeEOB, "i", "Q1"); err != nil {
		t.Fatal(err)
	}
	_, act, err = s.End()
	step("end", act, err)

	deadline := time.Now().Add(2 * time.Second)
	for {
		rec.mu.Lock()
		var got []string
		for _, e := range rec.entries {
			got = append(got, e.To)
		}
		rec.mu.Unlock()
		if strings.Join(got, "; ") == `"a b"@example.net; ab@example.net` {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rows for %v, want \"a b\"@example.net and ab@example.net", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
