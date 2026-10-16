package milter

import (
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-milter"

	"github.com/mixeme/selfpost/internal/store"
)

// senderMaps is a sender_login_maps file composed by hand in the format
// internal/postfix writes (key, blank, comma-separated logins), though not in
// its order (renderSenderLoginMaps sorts the keys): shop and news send as any
// address of their domains, billing only as two listed addresses of
// example.org, and two applications share shop.example.org.
const senderMaps = `@news.example.net news
@shop.example.org shop,shop2
desk@shop.example.org shop2
@kiosk.example.net kiosk
invoices@example.org billing
billing@example.org billing
`

func writeMaps(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "table")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// offer plays one message to the check and returns the reply at the end of
// the headers, with what was left for the log.
func offer(t *testing.T, bind *Bindings, login, envelope string, headers ...[2]string) (milter.Response, []store.SendLogEntry) {
	t.Helper()
	refused := make(chan store.SendLogEntry, 8)
	s := &senderSession{bind: bind, refused: refused}
	if _, err := s.MailFrom(envelope, mods(map[string]string{"{auth_authen}": login})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RcptTo("someone@elsewhere.example", mods(nil)); err != nil {
		t.Fatal(err)
	}
	for _, h := range headers {
		if _, err := s.Header(h[0], h[1], mods(nil)); err != nil {
			t.Fatal(err)
		}
	}
	// cleanup has given the message its queue id by the end of the headers.
	resp, err := s.Headers(nil, mods(map[string]string{"i": "7BCAF3C75DA0"}))
	if err != nil {
		t.Fatal(err)
	}
	close(refused)
	var rows []store.SendLogEntry
	for e := range refused {
		rows = append(rows, e)
	}
	return resp, rows
}

func replyText(r milter.Response) string {
	m := r.Response()
	return string(rune(m.Code)) + " " + strings.TrimRight(string(m.Data), "\x00")
}

func TestSenderCheck(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	from := func(v string) [2]string { return [2]string{"From", v} }

	cases := []struct {
		name     string
		login    string
		headers  [][2]string
		pass     bool
		wantText string // a part of the refusal
	}{
		{"own address", "shop", [][2]string{from("shop@shop.example.org")}, true, ""},
		{"own domain, any address, with a name", "shop", [][2]string{from(`"The Shop" <Orders@Shop.Example.Org>`)}, true, ""},
		{"second application of the domain", "shop2", [][2]string{from("x@shop.example.org")}, true, ""},
		{"a listed address", "billing", [][2]string{from("Invoices <invoices@example.org>")}, true, ""},
		{"a listed address with an extension", "billing", [][2]string{from("invoices+2026-march@example.org")}, true, ""},
		{"an extension, any address of the domain", "shop", [][2]string{from("orders+48213@shop.example.org")}, true, ""},
		{"header name in another case", "shop", [][2]string{{"FROM", "a@shop.example.org"}}, true, ""},
		{"its own name inside the display name", "shop", [][2]string{from(`"shop@shop.example.org" <a@shop.example.org>`)}, true, ""},
		{"an encoded display name", "shop", [][2]string{from("=?utf-8?b?0JzQsNCz0LDQt9C40L0=?= <a@shop.example.org>")}, true, ""},
		{"a folded line", "shop", [][2]string{from("\"A very long display name of the shop\"\r\n <a@shop.example.org>")}, true, ""},
		{"an encoded name, folded", "shop", [][2]string{from("=?utf-8?b?0JzQsNCz0LDQt9C40L0=?=\n\t<a@shop.example.org>")}, true, ""},
		{"a name in windows-1251", "shop", [][2]string{from("=?windows-1251?B?zODj4Ofo7Q==?= <a@shop.example.org>")}, true, ""},
		{"a name in koi8-r, quoted-printable", "shop", [][2]string{from("=?koi8-r?Q?=ED=C1=C7=C1=DA=C9=CE?= <a@shop.example.org>")}, true, ""},
		{"a comment after the address", "shop", [][2]string{from("a@shop.example.org (the shop)")}, true, ""},
		{"the leading space the check asks Postfix to keep", "shop", [][2]string{from(" a@shop.example.org")}, true, ""},
		// Whatever the name, a comment or a quoted local part holds: the
		// address a mail program reads is the one judged.
		{"another sender in the display name", "shop", [][2]string{from(`"director@example.org" <a@shop.example.org>`)}, true, ""},
		{"the server's name in a comment", "shop", [][2]string{from("a@shop.example.org (postmaster@mail.example.org)")}, true, ""},
		{"a domain with a key and no application, in the display name", "shop", [][2]string{from(`"x@empty.example.com" <a@shop.example.org>`)}, true, ""},
		{"a stranger's address in the display name", "shop", [][2]string{from(`"someone@gmail.example" <a@shop.example.org>`)}, true, ""},
		{"another sender in an encoded display name", "shop", [][2]string{from("=?utf-8?q?director=40example=2Eorg?= <a@shop.example.org>")}, true, ""},
		{"a stranger's address in a comment", "shop", [][2]string{from("a@shop.example.org (someone@gmail.example)")}, true, ""},
		{"an unquoted address as the display name", "shop", [][2]string{from("director@example.org <a@shop.example.org>")}, false, "as a mail program reads it"},
		{"another domain inside a quoted local part", "shop", [][2]string{from(`"director@example.org"@shop.example.org`)}, true, ""},
		{"no local part", "shop", [][2]string{from("@shop.example.org")}, false, "as a mail program reads it"},
		// Lines a lenient mail program could show as another address
		// (review of stage 2c, F1).
		{"another address before a colon", "shop", [][2]string{from("ceo@bank.example:me@shop.example.org")}, false, "as a mail program reads it"},
		{"a route-like colon inside the brackets", "shop", [][2]string{from("Name <ceo@bank.example:me@shop.example.org>")}, false, "as a mail program reads it"},
		{"a list inside the brackets", "shop", [][2]string{from("<b@evil.example, a@shop.example.org>")}, false, "as a mail program reads it"},
		{"an address with two @", "shop", [][2]string{from("ceo@bank.example@shop.example.org")}, false, "as a mail program reads it"},
		// An address bound to another application, its local part quoted
		// (F2): a reader and Postfix take "desk" for desk.
		{"another application's address, quoted", "shop", [][2]string{from(`"desk"@shop.example.org`)}, false, "not an address this application sends as"},
		{"its own bound address, quoted", "shop2", [][2]string{from(`"desk"@shop.example.org`)}, true, ""},
		// A look-alike of the domain in a character a Unicode fold maps
		// to ASCII (F3): it is not the application's.
		{"the Kelvin sign for k in the domain", "kiosk", [][2]string{from("a@\u212Aiosk.example.net")}, false, "not an address this application sends as"},
		{"the same domain in ASCII", "kiosk", [][2]string{from("a@kiosk.example.net")}, true, ""},

		{"another registered domain", "shop", [][2]string{from("director@example.org")}, false, "not an address this application sends as"},
		{"the server's own name", "shop", [][2]string{from("postmaster@mail.example.org")}, false, "not an address this application sends as"},
		{"a stranger's domain", "shop", [][2]string{from("someone@gmail.example")}, false, "not an address this application sends as"},
		{"an address off the list", "billing", [][2]string{from("director@example.org")}, false, "not an address this application sends as"},
		{"an address off the list, with an extension", "billing", [][2]string{from("director+invoices@example.org")}, false, "not an address this application sends as"},
		{"a listed name as the extension of another", "billing", [][2]string{from("+invoices@example.org")}, false, "not an address this application sends as"},
		{"a subdomain of its own", "shop", [][2]string{from("a@sub.shop.example.org")}, false, "not an address this application sends as"},
		{"no From: line", "shop", [][2]string{{"Subject", "hello"}}, false, "no From: line"},
		{"two From: lines", "shop", [][2]string{from("a@shop.example.org"), from("director@example.org")}, false, "more than one From: line"},
		{"two addresses in one line", "shop", [][2]string{from("a@shop.example.org, director@example.org")}, false, "one address"},
		{"not an address", "shop", [][2]string{from("the shop")}, false, "as a mail program reads it"},
		{"an address before a list comma, then another", "shop", [][2]string{from("director@example.org, <a@shop.example.org>")}, false, "one address"},
		// The key is the login's, so a long quoted local part naming another
		// domain picks nothing: OpenDKIM's 256-byte cut went with it.
		{"a long quoted local part naming another domain", "shop", [][2]string{from(`"` + strings.Repeat("a", 240) + `@example.org"@shop.example.org`)}, true, ""},
		{"a group", "shop", [][2]string{from("friends: a@shop.example.org, b@shop.example.org;")}, false, "one address"},
		{"another sender folded onto the next line", "shop", [][2]string{from("\"x\"\r\n <director@example.org>")}, false, "not an address this application sends as"},
		{"a quote left open before another address", "shop", [][2]string{from(`"a@shop.example.org <director@example.org>`)}, false, "as a mail program reads it"},
		{"no login", "", [][2]string{from("a@shop.example.org")}, false, "no authenticated application"},
		{"an unknown login", "ghost", [][2]string{from("a@shop.example.org")}, false, "not an address this application sends as"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, rows := offer(t, bind, c.login, "shop@shop.example.org", c.headers...)
			if c.pass {
				if resp != milter.RespContinue {
					t.Fatalf("refused: %s", replyText(resp))
				}
				if len(rows) != 0 {
					t.Fatalf("a passed message left rows: %+v", rows)
				}
				return
			}
			text := replyText(resp)
			if !strings.HasPrefix(text, "y 550 5.7.1 From: ") || !strings.Contains(text, c.wantText) {
				t.Fatalf("reply %q, want a 550 5.7.1 saying %q", text, c.wantText)
			}
			// The reason names no other domain of the server (audit L8); the
			// From: line before it is what the client wrote itself.
			reason := text[strings.LastIndex(text, " - ")+3:]
			for _, other := range []string{"example.com", "mail.example.org", "example.org"} {
				if strings.Contains(reason, other) {
					t.Fatalf("the reason %q names %s", reason, other)
				}
			}
			if len(rows) != 1 || rows[0].Reason != store.ReasonFrom || rows[0].To != "someone@elsewhere.example" || rows[0].AppLogin != c.login || rows[0].QueueID != "7BCAF3C75DA0" {
				t.Fatalf("log rows: %+v", rows)
			}
		})
	}
}

// What the client wrote is said back to it, logged and stored: it must not
// be able to put a line break or a control character there.
func TestSenderCheckPrintsNothingRaw(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	evil := "x@gmail.example\r\n250 ok %s \x00\x1b[31m " + strings.Repeat("a", 400)
	resp, rows := offer(t, bind, "shop", "shop@shop.example.org", [2]string{"From", evil})
	text := replyText(resp)
	for _, bad := range []string{"\r", "\n", "%", "\x1b"} {
		if strings.Contains(text, bad) || strings.Contains(rows[0].HeaderFrom, bad) {
			t.Fatalf("%q got through: %q / %q", bad, text, rows[0].HeaderFrom)
		}
	}
	if len(rows[0].HeaderFrom) > 130 {
		t.Fatalf("stored %d bytes of the header", len(rows[0].HeaderFrom))
	}
}

// Without its file the check knows nothing, and what cannot be checked is
// deferred - neither passed nor refused for good.
func TestSenderCheckDefersWithoutBindings(t *testing.T) {
	bind := NewBindings(filepath.Join(t.TempDir(), "absent"))
	resp, rows := offer(t, bind, "shop", "shop@shop.example.org", [2]string{"From", "a@shop.example.org"})
	if resp != milter.RespTempFail {
		t.Fatalf("reply %s, want a temporary failure", replyText(resp))
	}
	if len(rows) != 0 {
		t.Fatalf("a deferred message left rows: %+v", rows)
	}
}

// The panel rewrites the file when an application is added or deleted; the
// check has to follow without a restart.
func TestBindingsFollowTheFile(t *testing.T) {
	p := writeMaps(t, senderMaps)
	bind := NewBindings(p)
	if ok, err := bind.Allowed("shop", "a@shop.example.org"); err != nil || !ok {
		t.Fatalf("before: %v %v", ok, err)
	}
	if err := os.WriteFile(p, []byte("@news.example.net news\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A rewrite within the clock's resolution that keeps the size would not be
	// seen; the panel replaces the file by rename, which gives it a new time.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if ok, err := bind.Allowed("shop", "a@shop.example.org"); err != nil || ok {
		t.Fatalf("after the application was deleted: %v %v", ok, err)
	}
}

// The owners of the first key Postfix finds decide, not any key that lists
// the login: an address bound to one application is not open to another that
// holds its whole domain (Postfix 3.10.13, smtpd_check.c,
// reject_auth_sender_login_mismatch(), over mail_addr_find(); assumptions
// B-PS1). Composed by hand in internal/postfix's format.
func TestBindingsFirstKeyWins(t *testing.T) {
	bind := NewBindings(writeMaps(t, `@example.org shop
billing@example.org billing
invoices+march@example.org other
`))
	for _, c := range []struct {
		login, addr string
		want        bool
	}{
		{"shop", "a@example.org", true},
		{"billing", "billing@example.org", true},
		{"shop", "billing@example.org", false},        // the address's own key decides
		{"billing", "billing+q3@example.org", true},   // without the extension
		{"shop", "billing+q3@example.org", false},     // ... before @domain
		{"shop", "invoices+march@example.org", false}, // the full key first
		{"other", "invoices+march@example.org", true},
		{"shop", "invoices+april@example.org", true}, // no key without "+": @domain
		{"billing", "a@example.org", false},
		{"SHOP", "A@Example.ORG", true}, // strcasecmp_utf8, a folded key
	} {
		if ok, err := bind.Allowed(c.login, c.addr); err != nil || ok != c.want {
			t.Errorf("Allowed(%q, %q) = %v, %v; want %v", c.login, c.addr, ok, err, c.want)
		}
	}
}

// The table is read the way dict_thash_open() reads it: continuation lines,
// owners split on blanks as well as commas, comments, lines Postfix ignores.
// Composed by hand.
func TestBindingsReadAsPostfixReadsThem(t *testing.T) {
	bind := NewBindings(writeMaps(t, "# a comment\n"+
		"@shop.example.org shop,\n"+
		"  shop2 shop3\n"+
		"\n"+
		"\"a b\"@x.example.org quoted\n"+
		"nokey-value-only\n"+
		"\"open@x.example.org broken\n"+
		"@news.example.net\tnews\r\n"))
	for _, c := range []struct {
		login, addr string
		want        bool
	}{
		{"shop", "a@shop.example.org", true},
		{"shop2", "a@shop.example.org", true},
		{"shop3", "a@shop.example.org", true},
		{"news", "a@news.example.net", true},
		{"quoted", "\"a b\"@x.example.org", true},
		{"broken", "\"open@x.example.org", false},
	} {
		if ok, err := bind.Allowed(c.login, c.addr); err != nil || ok != c.want {
			t.Errorf("Allowed(%q, %q) = %v, %v; want %v", c.login, c.addr, ok, err, c.want)
		}
	}
}

// A key given twice - in any case, since Postfix folds keys - makes Postfix
// refuse the whole table (dict_thash_open(), no DICT_FLAG_DUP_* flag), and
// the check defers as smtpd does. Composed by hand.
func TestBindingsDuplicateKeyBreaksTheTable(t *testing.T) {
	bind := NewBindings(writeMaps(t, "@shop.example.org shop\nA@Shop.example.org other\n"))
	if _, err := bind.Allowed("shop", "b@shop.example.org"); err != nil {
		t.Fatalf("two different keys: %v", err)
	}
	bind = NewBindings(writeMaps(t, "@shop.example.org shop\n@SHOP.example.org other\n"))
	if ok, err := bind.Allowed("shop", "b@shop.example.org"); err == nil || ok {
		t.Fatalf("duplicate key: %v, %v; want an error", ok, err)
	}
}

// A second message on the connection is judged by its own headers - not by
// the h go-milter passes, which still holds the earlier messages' From: (it
// is emptied only on ABORT; audit 2026-10-12, L9).
func TestSenderCheckResetsBetweenMessages(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	s := &senderSession{bind: bind}
	auth := mods(map[string]string{"{auth_authen}": "shop"})
	h := textproto.MIMEHeader{}
	for i, v := range []string{"director@example.org", "a@shop.example.org", "director@example.org"} {
		_, _ = s.MailFrom("shop@shop.example.org", auth)
		h.Add("From", v)
		_, _ = s.Header("From", v, mods(nil))
		resp, _ := s.Headers(h, mods(nil))
		if passed := resp == milter.RespContinue; passed != (i == 1) {
			t.Fatalf("message %d (From: %s): passed=%v", i, v, passed)
		}
	}
}

// A refused message is one row of the log however many recipients it had.
func TestSenderCheckRefusalIsOneRow(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	refused := make(chan store.SendLogEntry, 8)
	s := &senderSession{bind: bind, refused: refused}
	_, _ = s.MailFrom("shop@shop.example.org", mods(map[string]string{"{auth_authen}": "shop"}))
	for _, r := range []string{"a@x.example", "b@x.example", "c@x.example"} {
		_, _ = s.RcptTo(r, mods(nil))
	}
	_, _ = s.Header("From", "director@example.org", mods(nil))
	_, _ = s.Headers(nil, mods(nil))
	close(refused)
	var rows []store.SendLogEntry
	for e := range refused {
		rows = append(rows, e)
	}
	if len(rows) != 1 || rows[0].To != "a@x.example and 2 more" {
		t.Fatalf("rows = %+v", rows)
	}
}

// A message with the null envelope sender - a bounce an application writes
// itself - is judged like any other, and its row belongs to the domain the
// login is bound to.
func TestSenderCheckRefusalWithNullSenderHasItsDomain(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	for login, want := range map[string]string{"shop": "shop.example.org", "billing": "example.org"} {
		_, rows := offer(t, bind, login, "", [2]string{"From", "someone@gmail.example"})
		if len(rows) != 1 || rows[0].Domain != want || rows[0].From != "" {
			t.Errorf("%s: rows = %+v, want one of domain %s", login, rows, want)
		}
	}
}

// passedFromLines reads testdata/from-lines-passed.txt with {domain} put in.
func passedFromLines(t *testing.T, domain string) []string {
	return fromLines(t, "from-lines-passed.txt", domain, "news.example.net")
}

// fromLines reads one of the shared files of From: lines with the
// placeholders put in: {domain}, {DOMAIN} (in capitals), {other}, {OTHER} and
// {other_qp} (its dots as quoted pairs).
func fromLines(t *testing.T, file, domain, other string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, err := strconv.Unquote(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		out = append(out, strings.NewReplacer(
			"{domain}", domain, "{DOMAIN}", strings.ToUpper(domain),
			"{other_qp}", strings.ReplaceAll(other, ".", `\.`),
			"{other}", other, "{OTHER}", strings.ToUpper(other),
		).Replace(s))
	}
	return out
}

// No line of the shared file of lines naming another sender is passed to be
// passed as that sender: a line the check passes is one a mail program reads
// as an address of the application's own domain.
func TestSenderCheckNeverPassesTheSharedLinesForAnother(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	for _, line := range fromLines(t, "from-lines-other.txt", "shop.example.org", "news.example.net") {
		resp, _ := offer(t, bind, "shop", "shop@shop.example.org", [2]string{"From", line})
		if resp != milter.RespContinue {
			continue
		}
		list, err := fromParser.ParseList(unfold(line))
		if err != nil || len(list) != 1 || domainOf(asciiLower(list[0].Address)) != "shop.example.org" {
			t.Errorf("From: %q passed, and a mail program reads it as %v (%v)", line, list, err)
		}
	}
}

// Every line of the shared file is passed for the domain's application; the
// e2e then checks each arrives signed for that domain alone.
func TestSenderCheckPassesTheSharedLines(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	for _, line := range passedFromLines(t, "shop.example.org") {
		resp, rows := offer(t, bind, "shop", "shop@shop.example.org", [2]string{"From", line})
		if resp != milter.RespContinue || len(rows) != 0 {
			t.Errorf("From: %q refused: %s", line, replyText(resp))
		}
	}
}

// A Sender: line, when the message has one, is held to the rule From: is
// held to; the From: line is judged first (the owner's decision, 2026-10-16).
func TestSenderLineHeldToTheSameRule(t *testing.T) {
	bind := NewBindings(writeMaps(t, senderMaps))
	from := [2]string{"From", "a@shop.example.org"}
	sender := func(v string) [2]string { return [2]string{"Sender", v} }
	for _, c := range []struct {
		name     string
		headers  [][2]string
		field    string // the line refused; "" when the message passes
		wantText string
	}{
		{"no Sender: line", [][2]string{from}, "", ""},
		{"its own address", [][2]string{from, sender("shop@shop.example.org")}, "", ""},
		{"its own address, with a name", [][2]string{from, sender(`"The Shop's mailer" <mailer@shop.example.org>`)}, "", ""},
		{"header name in another case", [][2]string{from, {"SENDER", "ceo@bank.example"}}, "Sender", "not an address this application sends as"},
		{"a stranger's address", [][2]string{from, sender("ceo@bank.example")}, "Sender", "not an address this application sends as"},
		{"another application's address", [][2]string{from, sender("desk@shop.example.org")}, "Sender", "not an address this application sends as"},
		{"two addresses", [][2]string{from, sender("a@shop.example.org, ceo@bank.example")}, "Sender", "one address"},
		{"two Sender: lines", [][2]string{from, sender("a@shop.example.org"), sender("b@shop.example.org")}, "Sender", "more than one Sender: line"},
		{"a wrong From: and a wrong Sender:", [][2]string{{"From", "ceo@bank.example"}, sender("x@bank.example")}, "From", "not an address this application sends as"},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, rows := offer(t, bind, "shop", "shop@shop.example.org", c.headers...)
			if c.field == "" {
				if resp != milter.RespContinue || len(rows) != 0 {
					t.Fatalf("refused: %s", replyText(resp))
				}
				return
			}
			text := replyText(resp)
			if !strings.HasPrefix(text, "y 550 5.7.1 "+c.field+": ") || !strings.Contains(text, c.wantText) {
				t.Fatalf("reply %q, want a 550 5.7.1 about %s: saying %q", text, c.field, c.wantText)
			}
			reason := store.ReasonFrom
			if c.field == "Sender" {
				reason = store.ReasonSender
			}
			if len(rows) != 1 || rows[0].Reason != reason {
				t.Fatalf("log rows: %+v, want one of reason %q", rows, reason)
			}
		})
	}
}
