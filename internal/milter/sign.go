package milter

// The DKIM signer (docs/plans/panel-dkim-signing.md): the panel's own milter
// that signs what applications send on 465/587 with the key of the domain
// their login belongs to, and what the panel itself sends with the key of
// the server's name. Each of the two is a socket of its own, so the socket a
// message comes through - not the absence of a login - decides that the
// server's key may sign it (Q1 of the plan).

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-milter"
	"github.com/emersion/go-msgauth/dkim"
)

// signHeaders is the set of header names signed when present: OpenDKIM's
// dkim_should_signhdrs[], RFC 6376 § 5.4.1's list (OpenDKIM 2.11.0~beta2,
// libopendkim/dkim.c:223-246), which OpenDKIM signed with until now. Every
// occurrence is signed, in message order, and From once more (signLines).
var signHeaders = map[string]bool{
	"from": true, "reply-to": true, "subject": true, "date": true, "to": true, "cc": true,
	"resent-date": true, "resent-from": true, "resent-sender": true, "resent-to": true, "resent-cc": true,
	"in-reply-to": true, "references": true,
	"list-id": true, "list-help": true, "list-unsubscribe": true, "list-subscribe": true,
	"list-post": true, "list-owner": true, "list-archive": true,
}

// signLimit bounds how long a message may take from the end of its headers
// to its end. go-milter v0.4.1 tells a session nothing when its connection
// ends early (session.go, HandleMilterCommands(): it returns on EOF or QUIT
// without calling Abort), and go-msgauth's signer reads the message in a
// goroutine of its own (dkim/sign.go, NewSigner(): an io.Pipe), which would
// wait for the rest for ever. At the limit the signer is closed and a message
// that does go on is deferred, never signed over part of its body. An hour is
// far above Postfix's own bounds on a transaction.
const signLimit = time.Hour

// SignKeys reads the domains the server signs for, their selectors and key
// files from the KeyTable the panel writes (internal/domain, renderTables():
// "<domain> <domain>:<selector>:<keysDir>/<domain>/<selector>.private"), the
// table OpenDKIM reads too until it leaves the image. A key file is read
// when it changes, and only from under keysDir.
type SignKeys struct {
	table, keysDir string

	mu    sync.Mutex
	stamp fileStamp
	rows  map[string]keyRow
	keys  map[string]loadedKey
}

type keyRow struct{ selector, path string }

type loadedKey struct {
	stamp  fileStamp
	signer crypto.Signer
}

// fileStamp tells a file's versions apart: the panel replaces a table or a
// key by writing a new file and renaming it over the old one
// (internal/atomicfile), so a new version is a new modification time or
// size.
type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(fi os.FileInfo) fileStamp { return fileStamp{fi.ModTime(), fi.Size()} }

// errNoKey is a domain the KeyTable has no line for.
var errNoKey = errors.New("no signing key")

// NewSignKeys reads keyTable; keysDir is the directory every key in it must
// be under.
func NewSignKeys(keyTable, keysDir string) *SignKeys {
	return &SignKeys{table: keyTable, keysDir: keysDir, keys: map[string]loadedKey{}}
}

// Lookup gives the selector and key of domain.
func (k *SignKeys) Lookup(domain string) (string, crypto.Signer, error) {
	domain = strings.ToLower(domain)
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.readTable(); err != nil {
		return "", nil, err
	}
	row, ok := k.rows[domain]
	if !ok {
		return "", nil, fmt.Errorf("%w for %s", errNoKey, domain)
	}
	fi, err := os.Stat(row.path)
	if err != nil {
		return "", nil, fmt.Errorf("key of %s: %w", domain, err)
	}
	if have, ok := k.keys[row.path]; ok && have.stamp == stampOf(fi) {
		return row.selector, have.signer, nil
	}
	signer, err := readKey(row.path)
	if err != nil {
		return "", nil, fmt.Errorf("key of %s: %w", domain, err)
	}
	k.keys[row.path] = loadedKey{stampOf(fi), signer}
	return row.selector, signer, nil
}

func (k *SignKeys) readTable() error {
	fi, err := os.Stat(k.table)
	if err != nil {
		return fmt.Errorf("key table: %w", err)
	}
	if k.rows != nil && k.stamp == stampOf(fi) {
		return nil
	}
	data, err := os.ReadFile(k.table)
	if err != nil {
		return fmt.Errorf("key table: %w", err)
	}
	rows := map[string]keyRow{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, ok := strings.Cut(line, " ")
		// Domain and selector never hold a colon (internal/domain,
		// opendkimForbidden); the path takes the rest.
		parts := strings.SplitN(value, ":", 3)
		if !ok || len(parts) != 3 || !strings.EqualFold(parts[0], name) {
			return fmt.Errorf("key table: line %d is not \"<domain> <domain>:<selector>:<path>\"", n+1)
		}
		domain, selector, path := strings.ToLower(name), parts[1], parts[2]
		// The panel writes exactly this path (renderTables); anything else
		// is not read.
		if want := filepath.Join(k.keysDir, domain, selector+".private"); filepath.Clean(path) != want {
			return fmt.Errorf("key table: line %d names %s, not %s", n+1, path, want)
		}
		rows[domain] = keyRow{selector, path}
	}
	k.rows, k.stamp = rows, stampOf(fi)
	return nil
}

// readKey reads a PEM private key: PKCS#1 as internal/domain writes it
// (dkim.go, writePrivateKeyPEM()), PKCS#8 as well.
func readKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not an RSA or PKCS#8 key: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("key type %T cannot sign", key)
	}
	return signer, nil
}

// signSession is one connection of a signer socket.
type signSession struct {
	milter.NoOpMilter
	keys *SignKeys
	// domainOf gives the domain whose key signs the message: the login's on
	// the applications' socket, the server's name on the panel's own.
	domainOf func(login string) string

	login   string
	headers []field
	signer  *dkim.Signer
	domain  string
	expired chan struct{} // closed by the watchdog at signLimit
	done    chan struct{} // closed when the message ends, to stop it
}

type field struct{ name, value string }

func (s *signSession) reset() {
	if s.signer != nil {
		close(s.done)
		_ = s.signer.Close()
	}
	s.login, s.headers, s.signer, s.domain = "", nil, nil, ""
}

// MailFrom starts a message. {auth_authen} is the SASL login (see
// session.MailFrom in milter.go for the macro list Postfix sends).
func (s *signSession) MailFrom(from string, m *milter.Modifier) (milter.Response, error) {
	s.reset()
	s.login = macro(m, "auth_authen")
	return milter.RespContinue, nil
}

// Header keeps every header as Postfix hands it over. Postfix 3.10.13,
// src/milter/milter8.c:2329-2400, milter8_header(): the value with its leading
// white space, for a milter that asks for it (OptHeaderLeadingSpace,
// ServeSign); a header of several lines as text separated by "\n"; and not
// Postfix's own Received:, which goes above the signature anyway (an
// inserted header is placed relative to the message as delivered).
func (s *signSession) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	s.headers = append(s.headers, field{name, value})
	return milter.RespContinue, nil
}

// Headers chooses the key and starts the signer with the header block.
// What cannot be signed is deferred: the socket is attached with
// default_action=tempfail, and so is every refusal here.
//
// h is not read: go-milter v0.4.1 keeps every header of the connection in it
// (see senderSession.Headers).
func (s *signSession) Headers(h textproto.MIMEHeader, m *milter.Modifier) (milter.Response, error) {
	s.domain = s.domainOf(s.login)
	if s.domain == "" {
		log.Printf("dkim-signer: no sending domain for login %q - deferring", s.login)
		return milter.RespTempFail, nil
	}
	selector, key, err := s.keys.Lookup(s.domain)
	if err != nil {
		log.Printf("dkim-signer: %v - deferring", err)
		return milter.RespTempFail, nil
	}
	keys, block := signLines(s.headers)
	signer, err := dkim.NewSigner(&dkim.SignOptions{
		Domain:                 s.domain,
		Selector:               selector,
		Signer:                 key,
		HeaderCanonicalization: dkim.CanonicalizationRelaxed,
		BodyCanonicalization:   dkim.CanonicalizationSimple,
		HeaderKeys:             keys,
	})
	if err != nil {
		log.Printf("dkim-signer: %s: %v - deferring", s.domain, err)
		return milter.RespTempFail, nil
	}
	s.signer, s.expired, s.done = signer, make(chan struct{}), make(chan struct{})
	go watch(signer, s.expired, s.done)
	if _, err := signer.Write(block); err != nil {
		log.Printf("dkim-signer: %s: %v - deferring", s.domain, err)
		s.reset()
		return milter.RespTempFail, nil
	}
	return milter.RespContinue, nil
}

// watch closes signer at signLimit unless done is closed first.
func watch(signer *dkim.Signer, expired, done chan struct{}) {
	t := time.NewTimer(signLimit)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		close(expired)
		_ = signer.Close()
	}
}

// signLines is the header block the signer reads and the names it signs.
// Each header is written as Postfix will deliver it: the name, the colon, the
// value with its own leading white space, the lines of a folded value joined
// with CRLF; and the empty line that ends the block. The names: every header
// in signHeaders, in message order, and From once more, so that a From:
// added later breaks the signature (RFC 6376 § 5.4.2; OversignHeaders From in
// OpenDKIM's configuration until now; go-msgauth v0.7.0, dkim/sign.go,
// NewSigner(): a name with no instance left is kept in h= and hashes
// nothing).
func signLines(headers []field) ([]string, []byte) {
	var keys []string
	var b strings.Builder
	for _, f := range headers {
		b.WriteString(f.name)
		b.WriteString(":")
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(f.value, "\r\n", "\n"), "\n", "\r\n"))
		b.WriteString("\r\n")
		if signHeaders[strings.ToLower(strings.TrimSpace(f.name))] {
			keys = append(keys, strings.TrimSpace(f.name))
		}
	}
	b.WriteString("\r\n")
	return append(keys, "From"), []byte(b.String())
}

// BodyChunk hashes the body as it comes. Postfix 3.10.13,
// src/milter/milter8.c:2470-2500, milter8_body(): every line ends "\r\n".
func (s *signSession) BodyChunk(chunk []byte, m *milter.Modifier) (milter.Response, error) {
	if s.signer == nil {
		return milter.RespContinue, nil
	}
	if _, err := s.signer.Write(chunk); err != nil {
		log.Printf("dkim-signer: %s: %v - deferring", s.domain, err)
		s.reset()
		return milter.RespTempFail, nil
	}
	return milter.RespContinue, nil
}

// Body is the end of the message: the signature goes in at the top, where
// OpenDKIM put it (opendkim/opendkim.c:3673, dkimf_insheader(ctx, 0, ...)).
// Postfix 3.10.13, src/milter/milter8.c:1445-1465, SMFIR_INSHEADER: the
// index is passed on plus one; src/cleanup/cleanup_milter.c:1193,
// cleanup_ins_header(): "Index 1 is the top-most header" - above Postfix's
// own Received:, which a milter never sees (milter8.c:2352-2358, "insert
// requests are relative to the message as delivered"). go-milter v0.4.1,
// modifier.go, InsertHeader() turns the folded value's CRLF into LF, as
// Postfix keeps a header.
func (s *signSession) Body(m *milter.Modifier) (milter.Response, error) {
	signer := s.signer
	if signer == nil {
		// Headers did not start one, and answered tempfail already.
		return milter.RespTempFail, nil
	}
	close(s.done)
	s.signer = nil
	err := signer.Close()
	select {
	case <-s.expired:
		err = fmt.Errorf("the message took longer than %s", signLimit)
	default:
	}
	if err != nil {
		log.Printf("dkim-signer: %s: %v - deferring", s.domain, err)
		return milter.RespTempFail, nil
	}
	name, value, ok := strings.Cut(signer.Signature(), ": ")
	if !ok || !strings.EqualFold(name, "DKIM-Signature") {
		log.Printf("dkim-signer: %s: malformed signature from the library - deferring", s.domain)
		return milter.RespTempFail, nil
	}
	if err := m.InsertHeader(0, name, strings.TrimSuffix(value, "\r\n")); err != nil {
		return nil, err
	}
	return milter.RespContinue, nil
}

func (s *signSession) Abort(m *milter.Modifier) error {
	s.reset()
	return nil
}

// ServeSign runs a signer on ln until ctx is cancelled; domainOf chooses the
// key of each message (signSession.domainOf).
//
// The protocol: headers with their leading space (as for the sender check,
// ServeSender) and the body. The milter says it adds headers (go-milter
// v0.4.1, milter.go: OptAddHeader is SMFIF_ADDHDRS), as the protocol asks of
// one that does; Postfix 3.10.13 does not hold an insert request against it
// (src/milter/milter8.c:1445-1465 reads SMFIR_INSHEADER without looking at
// the flag).
func ServeSign(ctx context.Context, ln net.Listener, keys *SignKeys, domainOf func(login string) string) error {
	srv := &milter.Server{
		NewMilter: func() milter.Milter {
			return &signSession{keys: keys, domainOf: domainOf}
		},
		Actions:  milter.OptAddHeader,
		Protocol: milter.OptHeaderLeadingSpace,
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}
