package milter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-milter"
	"github.com/emersion/go-msgauth/dkim"
)

// signFixture is a key directory and selector table laid out as
// internal/domain writes them (renderSelectors(), writePrivateKeyPEM(): PKCS#1 PEM) for
// example.org with selector sel, and the DNS record of the key.
type signFixture struct {
	dir, table, keysDir string
	record              string
}

func newSignFixture(t *testing.T) signFixture {
	t.Helper()
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(keysDir, "example.org", "sel.private")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	table := filepath.Join(dir, "selectors")
	if err := os.WriteFile(table, []byte("example.org sel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return signFixture{dir, table, keysDir, "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pub)}
}

// startSigner serves a signer whose logins map by domains and returns a
// session of go-milter's client on it.
func startSigner(t *testing.T, f signFixture, domainOf func(string) string) *milter.ClientSession {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go ServeSign(ctx, ln, NewSignKeys(f.table, f.keysDir), domainOf)
	// The library's server speaks milter version 2; the client asks for no
	// action only version 6 has (see TestRecipientsThroughGoMilter).
	cl := milter.NewClientWithOptions("tcp", ln.Addr().String(), milter.ClientOptions{
		ActionMask: milter.OptAddHeader | milter.OptChangeHeader,
	})
	t.Cleanup(func() { cl.Close() })
	s, err := cl.Session()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func appDomains(login string) string {
	if login == "app1" {
		return "example.org"
	}
	return ""
}

// The headers of a test message as Postfix hands them to a milter that asks
// for the leading space: the value after the colon, a folded one with "\n"
// between its lines (Postfix 3.10.13, src/milter/milter8.c:2329-2400,
// milter8_header()). Composed from that source, not seen on a stand.
var testHeaders = []field{
	{"From", " Alice <alice@example.org>"},
	{"To", " bob@example.net"},
	{"Subject", " a subject that is\n\tfolded over two lines"},
	{"Date", " Thu, 15 Oct 2026 20:00:00 +0000"},
	{"Message-ID", " <1@example.org>"},
	{"X-Mailer", "no space after the colon"},
}

const testBody = "Hello,\r\n\r\nthe body, with a blank line in it.\r\n"

// sendMessage runs one message through s and returns the modifications
// asked for at its end and the final action.
func sendMessage(t *testing.T, s *milter.ClientSession, login string, headers []field, body string) ([]milter.ModifyAction, *milter.Action) {
	t.Helper()
	if err := s.Macros(milter.CodeMail, "{auth_authen}", login); err != nil {
		t.Fatal(err)
	}
	if act, err := s.Mail("<alice@example.org>", nil); err != nil || act.Code != milter.ActContinue {
		t.Fatalf("mail: %+v %v", act, err)
	}
	if act, err := s.Rcpt("<bob@example.net>", nil); err != nil || act.Code != milter.ActContinue {
		t.Fatalf("rcpt: %+v %v", act, err)
	}
	for _, h := range headers {
		if act, err := s.HeaderField(h.name, h.value); err != nil || act.Code != milter.ActContinue {
			t.Fatalf("header %s: %+v %v", h.name, act, err)
		}
	}
	act, err := s.HeaderEnd()
	if err != nil {
		t.Fatal(err)
	}
	if act.Code != milter.ActContinue {
		return nil, act
	}
	if act, err := s.BodyChunk([]byte(body)); err != nil || act.Code != milter.ActContinue {
		t.Fatalf("body: %+v %v", act, err)
	}
	mods, act, err := s.End()
	if err != nil {
		t.Fatal(err)
	}
	return mods, act
}

// delivered is the message as Postfix delivers it with the signature
// inserted at the top: every header line ending CRLF, and the signature as
// "name:value" - its value brings its own space (Postfix 3.10.13,
// src/cleanup/cleanup_milter.c:1085, "%s:%s%s" with no space for a milter
// that asked for SMFIP_HDR_LEADSPC).
func delivered(sig milter.ModifyAction, headers []field, body string) []byte {
	var b bytes.Buffer
	crlf := func(v string) string { return strings.ReplaceAll(v, "\n", "\r\n") }
	b.WriteString(sig.HeaderName + ":" + crlf(sig.HeaderValue) + "\r\n")
	for _, h := range headers {
		b.WriteString(h.name + ":" + crlf(h.value) + "\r\n")
	}
	b.WriteString("\r\n" + body)
	return b.Bytes()
}

func verifyWith(t *testing.T, record string, msg []byte) *dkim.Verification {
	t.Helper()
	vs, err := dkim.VerifyWithOptions(bytes.NewReader(msg), &dkim.VerifyOptions{
		LookupTXT: func(string) ([]string, error) { return []string{record}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("%d signatures, want 1", len(vs))
	}
	return vs[0]
}

// signed sends one message and returns the signature the signer inserted.
func signed(t *testing.T, s *milter.ClientSession, login string, headers []field, body string) milter.ModifyAction {
	t.Helper()
	mods, act := sendMessage(t, s, login, headers, body)
	if act.Code != milter.ActContinue {
		t.Fatalf("end of message: %+v, want continue", act)
	}
	if len(mods) != 1 || mods[0].Code != milter.ActInsertHeader || mods[0].HeaderIndex != 0 || mods[0].HeaderName != "DKIM-Signature" {
		t.Fatalf("modifications %+v, want one DKIM-Signature inserted at 0", mods)
	}
	return mods[0]
}

func TestSignerSignsWithTheLoginsDomain(t *testing.T) {
	f := newSignFixture(t)
	s := startSigner(t, f, appDomains)
	sig := signed(t, s, "app1", testHeaders, testBody)

	v := verifyWith(t, f.record, delivered(sig, testHeaders, testBody))
	if v.Err != nil {
		t.Fatalf("signature does not verify: %v\n%s", v.Err, delivered(sig, testHeaders, testBody))
	}
	if v.Domain != "example.org" {
		t.Errorf("d=%s, want example.org", v.Domain)
	}
	if !strings.HasPrefix(sig.HeaderValue, " ") {
		t.Errorf("the signature's value does not bring its space: %q", sig.HeaderValue)
	}
	for _, want := range []string{"c=relaxed/simple;", "s=sel;", "h=From:To:Subject:Date:Message-ID:To:Subject:Date:From;"} {
		if !strings.Contains(strings.Join(strings.Fields(sig.HeaderValue), ""), want) {
			t.Errorf("signature lacks %q: %s", want, sig.HeaderValue)
		}
	}
	// A second From:, Subject: or To: added on the way breaks it: each is
	// over-signed.
	for _, added := range []field{{"From", " Mallory <m@evil.example>"}, {"Subject", " urgent: wire the money"}, {"To", " victim@example.net"}} {
		tampered := append([]field{added}, testHeaders...)
		if v := verifyWith(t, f.record, delivered(sig, tampered, testBody)); v.Err == nil {
			t.Errorf("a %s: added above still verifies", added.name)
		}
	}
	// A Cc: the message had none of may be added: only what is present is
	// over-signed, as Postfix's cleanup adds a missing Date: after the
	// milters.
	withCc := append([]field{{"Cc", " someone@example.net"}}, testHeaders...)
	if v := verifyWith(t, f.record, delivered(sig, withCc, testBody)); v.Err != nil {
		t.Errorf("a Cc: the message did not have breaks it: %v", v.Err)
	}
	// A changed body breaks it.
	if v := verifyWith(t, f.record, delivered(sig, testHeaders, testBody+"more\r\n")); v.Err == nil {
		t.Error("a changed body still verifies")
	}
}

// Two messages on one connection are signed each on their own.
func TestSignerSignsEachMessageOfAConnection(t *testing.T) {
	f := newSignFixture(t)
	s := startSigner(t, f, appDomains)
	for i, body := range []string{testBody, "Another body.\r\n"} {
		sig := signed(t, s, "app1", testHeaders, body)
		if v := verifyWith(t, f.record, delivered(sig, testHeaders, body)); v.Err != nil {
			t.Fatalf("message %d: %v", i+1, v.Err)
		}
	}
}

// An empty body and a body without a final line end are signed as Postfix
// delivers them.
func TestSignerSignsOddBodies(t *testing.T) {
	f := newSignFixture(t)
	for _, body := range []string{"", "no final line end"} {
		s := startSigner(t, f, appDomains)
		sig := signed(t, s, "app1", testHeaders, body)
		if v := verifyWith(t, f.record, delivered(sig, testHeaders, body)); v.Err != nil {
			t.Fatalf("body %q: %v", body, v.Err)
		}
	}
}

// The panel's own socket signs with the server's name whatever the login.
func TestSignerOnTheLocalSocketUsesItsDomain(t *testing.T) {
	f := newSignFixture(t)
	s := startSigner(t, f, func(string) string { return "example.org" })
	sig := signed(t, s, "", testHeaders, testBody)
	if v := verifyWith(t, f.record, delivered(sig, testHeaders, testBody)); v.Err != nil || v.Domain != "example.org" {
		t.Fatalf("d=%s err=%v", v.Domain, v.Err)
	}
}

// What cannot be signed is deferred, never passed on unsigned.
func TestSignerDefersWhatItCannotSign(t *testing.T) {
	cases := []struct {
		name  string
		login string
		setup func(t *testing.T, f signFixture)
	}{
		{"a login with no domain", "stranger", nil},
		{"no line for the domain", "app1", func(t *testing.T, f signFixture) {
			if err := os.WriteFile(f.table, []byte("other.example sel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a key file that is gone", "app1", func(t *testing.T, f signFixture) {
			if err := os.Remove(filepath.Join(f.keysDir, "example.org", "sel.private")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a key path outside the key directory", "app1", func(t *testing.T, f signFixture) {
			outside := filepath.Join(f.dir, "sel.private")
			if err := os.Rename(filepath.Join(f.keysDir, "example.org", "sel.private"), outside); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.table, []byte("example.org ../../sel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a name of dots", "app1", func(t *testing.T, f signFixture) {
			if err := os.WriteFile(f.table, []byte("example.org sel\n.. sel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a selector table that is not one", "app1", func(t *testing.T, f signFixture) {
			if err := os.WriteFile(f.table, []byte("example.org\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"an old KeyTable line", "app1", func(t *testing.T, f signFixture) {
			line := "example.org example.org:sel:" + filepath.Join(f.keysDir, "example.org", "sel.private") + "\n"
			if err := os.WriteFile(f.table, []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSignFixture(t)
			if c.setup != nil {
				c.setup(t, f)
			}
			s := startSigner(t, f, appDomains)
			mods, act := sendMessage(t, s, c.login, testHeaders, testBody)
			if act.Code != milter.ActTempFail || len(mods) != 0 {
				t.Fatalf("got %+v with %d modifications, want tempfail and none", act, len(mods))
			}
		})
	}
}

// Two To: lines and the headers OpenDKIM's list left out verify as signed.
func TestSignerSignsRepeatedHeaders(t *testing.T) {
	f := newSignFixture(t)
	s := startSigner(t, f, appDomains)
	headers := []field{
		{"From", " Alice <alice@example.org>"}, {"Sender", " list@example.org"},
		{"To", " bob@example.net"}, {"To", " carol@example.net"}, {"Cc", " dave@example.net"},
		{"Subject", " two To lines"},
		{"List-Unsubscribe", " <https://example.org/u/1>"}, {"List-Unsubscribe-Post", " List-Unsubscribe=One-Click"},
	}
	sig := signed(t, s, "app1", headers, testBody)
	if v := verifyWith(t, f.record, delivered(sig, headers, testBody)); v.Err != nil {
		t.Fatalf("signature does not verify: %v", v.Err)
	}
	// The second To: changed: it was signed too.
	changed := append([]field(nil), headers...)
	changed[3] = field{"To", " mallory@example.net"}
	if v := verifyWith(t, f.record, delivered(sig, changed, testBody)); v.Err == nil {
		t.Error("a changed second To: still verifies")
	}
}

// A header section past headerLimit is refused for good, before any key is
// looked up, and nothing is inserted.
func TestSignerRefusesAHugeHeaderSection(t *testing.T) {
	f := newSignFixture(t)
	s := startSigner(t, f, appDomains)
	headers := []field{{"From", " Alice <alice@example.org>"}}
	filler := strings.Repeat("x", 50000)
	for i := 0; i*50000 < headerLimit; i++ {
		headers = append(headers, field{"X-Filler", " " + filler})
	}
	mods, act := sendMessage(t, s, "app1", headers, testBody)
	if act.Code != milter.ActReplyCode || act.SMTPCode != 552 || len(mods) != 0 {
		t.Fatalf("got %+v with %d modifications, want a 552 reply and none", act, len(mods))
	}
}

// signLines writes the block Postfix will deliver and names what is signed.
func TestSignLines(t *testing.T) {
	keys, block := signLines(testHeaders)
	if got := strings.Join(keys, ":"); got != "From:To:Subject:Date:Message-ID:To:Subject:Date:From" {
		t.Errorf("keys %s", got)
	}
	// Every occurrence is signed; a shown one is named once more, once.
	keys, _ = signLines([]field{
		{"From", " a@example.org"}, {"Sender", " b@example.org"}, {"To", " c@example.net"}, {"To", " d@example.net"},
		{"List-Unsubscribe", " <https://example.org/u>"}, {"List-Unsubscribe-Post", " List-Unsubscribe=One-Click"},
		{"X-Other", " not signed"},
	})
	if got := strings.Join(keys, ":"); got != "From:Sender:To:To:List-Unsubscribe:List-Unsubscribe-Post:Sender:To:From" {
		t.Errorf("keys %s", got)
	}
	want := "From: Alice <alice@example.org>\r\nTo: bob@example.net\r\n" +
		"Subject: a subject that is\r\n\tfolded over two lines\r\n" +
		"Date: Thu, 15 Oct 2026 20:00:00 +0000\r\nMessage-ID: <1@example.org>\r\n" +
		"X-Mailer:no space after the colon\r\n\r\n"
	if string(block) != want {
		t.Errorf("block\n%q\nwant\n%q", block, want)
	}
}

// Content-Type, MIME-Version and Message-ID are signed when present and not
// over-signed (security audit 2026-10-17, M6); a header line past the SMTP
// client's limit is named in the log (M8).
func TestSignLinesCoverTheMIMEHeadersAndLongHeaderLinesAreNamed(t *testing.T) {
	keys, _ := signLines([]field{
		{"From", " a@example.org"}, {"MIME-Version", " 1.0"}, {"Content-Type", " text/plain"}, {"Message-ID", " <x@example.org>"},
	})
	if got := strings.Join(keys, ":"); got != "From:MIME-Version:Content-Type:Message-ID:From" {
		t.Errorf("h= %s", got)
	}
	if got := longHeaderLine([]field{{"Subject", " " + strings.Repeat("x", 998)}}); got != "Subject" {
		t.Errorf("a 1007-byte Subject line: %q", got)
	}
	if got := longHeaderLine([]field{{"To", " a@example.org,\n " + strings.Repeat("b", 999)}}); got != "To" {
		t.Errorf("a long folded line: %q", got)
	}
	if got := longHeaderLine([]field{{"Subject", " short"}, {"To", " a,\n b"}}); got != "" {
		t.Errorf("short lines named %q", got)
	}
}
