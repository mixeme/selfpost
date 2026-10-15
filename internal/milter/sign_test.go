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

// signFixture is a key directory and KeyTable laid out as internal/domain
// writes them (renderTables(), writePrivateKeyPEM(): PKCS#1 PEM) for
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
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o750); err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, pemBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	table := filepath.Join(dir, "KeyTable")
	if err := os.WriteFile(table, []byte("example.org example.org:sel:"+keyPath+"\n"), 0o640); err != nil {
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
// inserted at the top: every header line ending CRLF.
func delivered(sig milter.ModifyAction, headers []field, body string) []byte {
	var b bytes.Buffer
	crlf := func(v string) string { return strings.ReplaceAll(v, "\n", "\r\n") }
	b.WriteString(sig.HeaderName + ": " + crlf(sig.HeaderValue) + "\r\n")
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
	for _, want := range []string{"c=relaxed/simple", "s=sel", "h=From:To:Subject:Date:From"} {
		if !strings.Contains(strings.ReplaceAll(sig.HeaderValue, "\n", ""), want) {
			t.Errorf("signature lacks %q: %s", want, sig.HeaderValue)
		}
	}
	// A second From: added on the way breaks it: From is over-signed.
	tampered := append([]field{{"From", " Mallory <m@evil.example>"}}, testHeaders...)
	if v := verifyWith(t, f.record, delivered(sig, tampered, testBody)); v.Err == nil {
		t.Error("a From: added above still verifies")
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
			if err := os.WriteFile(f.table, []byte("other.example other.example:sel:"+filepath.Join(f.keysDir, "other.example", "sel.private")+"\n"), 0o640); err != nil {
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
			if err := os.WriteFile(f.table, []byte("example.org example.org:sel:"+outside+"\n"), 0o640); err != nil {
				t.Fatal(err)
			}
		}},
		{"a key table that is not one", "app1", func(t *testing.T, f signFixture) {
			if err := os.WriteFile(f.table, []byte("example.org\n"), 0o640); err != nil {
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

// signLines writes the block Postfix will deliver and names what is signed.
func TestSignLines(t *testing.T) {
	keys, block := signLines(testHeaders)
	if got := strings.Join(keys, ":"); got != "From:To:Subject:Date:From" {
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
