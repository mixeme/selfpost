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

// signHeaders is the set of header names signed when present: RFC 6376
// § 5.4.1's list, as OpenDKIM's dkim_should_signhdrs[] held it (OpenDKIM
// 2.11.0~beta2, libopendkim/dkim.c:223-246), which SelfPost signed with until
// 2.8.0; and those that list leaves out: Sender, which a mail program shows
// beside From ("on behalf of"), List-Unsubscribe-Post, which RFC 8058 § 4
// wants signed for a one-click unsubscribe to be honoured, and since 2.14.4
// Content-Type, MIME-Version and Message-ID (security audit 2026-10-17, M6):
// unsigned, a relay could change a MIME boundary or the type and the
// signature still verified. Message-ID is not over-signed: cleanup adds one a
// message lacks after the milters, and a name in h= that is absent would
// refuse it. Every
// occurrence is signed, in message order, and the shown ones once more
// (signLines).
var signHeaders = map[string]bool{
	"from": true, "sender": true, "reply-to": true, "subject": true, "date": true, "to": true, "cc": true,
	"resent-date": true, "resent-from": true, "resent-sender": true, "resent-to": true, "resent-cc": true,
	"in-reply-to": true, "references": true,
	"list-id": true, "list-help": true, "list-unsubscribe": true, "list-unsubscribe-post": true,
	"list-subscribe": true, "list-post": true, "list-owner": true, "list-archive": true,
	"content-type": true, "mime-version": true, "message-id": true,
}

// overSign are the signed headers a mail program shows: each that the message
// has is named once more in h=, so that one added after signing breaks the
// signature (RFC 6376 § 5.4.2, § 8.15). Only those present: Postfix's cleanup
// adds a Date: or Message-ID: a message lacks after the milters, out of their
// sight (src/cleanup/cleanup_message.c:722, cleanup_header_done_callback(); a
// header cleanup adds is hidden from milters, cleanup_milter.c:758,
// hidden_header), and an absent name in h= would refuse it. From is present
// in every message the signer signs (the sender check refuses one without;
// the panel writes its own) and comes last.
var overSign = map[string]bool{"subject": true, "date": true, "to": true, "cc": true, "reply-to": true, "sender": true}

// headerLimit bounds the header section the signer keeps for a message. The
// signer holds it whole until the body starts (go-msgauth hashes headers at
// the end of the block), and Postfix bounds one header (60000 bytes with
// milters, src/cleanup/cleanup_message.c, KLUDGE_HEADER_LIMIT) but not their
// number: a message of nothing but headers up to message_size_limit, sent on
// fifty connections at once, would cost the panel gigabytes (security review
// of panel-dkim-signing). A mebibyte is far beyond any mail program's.
const headerLimit = 1 << 20

// smtpLineLimit is the longest line Postfix's SMTP client sends as it is:
// smtp_line_length_limit, 998 by default (Postfix 3.10.13,
// src/global/mail_params.h:1188), past which src/smtp/smtp_proto.c:1266-1274
// breaks the line and starts the rest with a space - after the signer hashed
// it whole, so the body hash no longer matches.
const smtpLineLimit = 998

// signLimit bounds how long a message may take from the end of its headers
// to its end. go-milter v0.4.1 tells a session nothing when its connection
// ends early (session.go, HandleMilterCommands(): it returns on EOF or QUIT
// without calling Abort), and go-msgauth's signer reads the message in a
// goroutine of its own (dkim/sign.go, NewSigner(): an io.Pipe), which would
// wait for the rest for ever. At the limit the signer is closed and a message
// that does go on is deferred, never signed over part of its body. An hour is
// far above Postfix's own bounds on a transaction.
const signLimit = time.Hour

// SignKeys reads the names the server signs for and their selectors from the
// selector table the panel writes (internal/domain, renderSelectors(): a line
// "<name> <selector>" each); the key of a name is
// keysDir/<name>/<selector>.private (domain.KeyPath). The table, a few
// lines, is read for every message: a stamp of modification time and size
// would miss a rewrite of the same size within one tick of the file system's
// clock (review of panel-dkim-signing, F7). A key is parsed again when its
// file changes, and forgotten when the table no longer names it.
type SignKeys struct {
	table, keysDir string

	mu   sync.Mutex
	rows map[string]keyRow
	keys map[string]loadedKey
}

type keyRow struct{ selector, path string }

type loadedKey struct {
	stamp  fileStamp
	signer crypto.Signer
}

// fileStamp tells a key file's versions apart: the panel replaces a key by
// writing a new file and renaming it over the old one (internal/atomicfile),
// so a new version is a new modification time or size - a key of the same
// size written within the same tick as the one before it is not told apart,
// which a person replacing a key by hand in the panel cannot do.
type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(fi os.FileInfo) fileStamp { return fileStamp{fi.ModTime(), fi.Size()} }

// errNoKey is a domain the selector table has no line for.
var errNoKey = errors.New("no signing key")

// NewSignKeys reads the selector table at selectors and the keys under
// keysDir.
func NewSignKeys(selectors, keysDir string) *SignKeys {
	return &SignKeys{table: selectors, keysDir: keysDir, keys: map[string]loadedKey{}}
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
	data, err := os.ReadFile(k.table)
	if err != nil {
		return fmt.Errorf("selector table: %w", err)
	}
	rows := map[string]keyRow{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, selector, ok := strings.Cut(line, " ")
		// The two are names of a directory and a file under keysDir: nothing
		// that would lead out of it (internal/domain, assertConfigSafe()).
		if !ok || !pathToken(name) || !pathToken(selector) {
			return fmt.Errorf("selector table: line %d is not \"<name> <selector>\"", n+1)
		}
		domain := strings.ToLower(name)
		rows[domain] = keyRow{selector, filepath.Join(k.keysDir, domain, selector+".private")}
	}
	k.rows = rows
	// A deleted domain's key does not stay in memory.
	named := map[string]bool{}
	for _, r := range rows {
		named[r.path] = true
	}
	for path := range k.keys {
		if !named[path] {
			delete(k.keys, path)
		}
	}
	return nil
}

// pathToken reports whether v can name a directory or a file of the key
// tree: not empty, no separator or white space, not dots alone.
func pathToken(v string) bool {
	return v != "" && !strings.ContainsAny(v, " \t\r\n:/\\") && strings.Trim(v, ".") != ""
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
	size    int  // the header section's bytes so far
	tooBig  bool // past headerLimit: refused at the end of the headers
	signer  *dkim.Signer
	domain  string
	line    int           // bytes of the body line being read
	long    bool          // a body line longer than smtpLineLimit was logged
	expired chan struct{} // closed by the watchdog at signLimit
	done    chan struct{} // closed when the message ends, to stop it
}

type field struct{ name, value string }

func (s *signSession) reset() {
	if s.signer != nil {
		close(s.done)
		_ = s.signer.Close()
	}
	s.login, s.headers, s.size, s.tooBig, s.signer, s.domain = "", nil, 0, false, nil, ""
	s.line, s.long = 0, false
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
//
// Past headerLimit nothing more is kept, and Headers refuses the message.
func (s *signSession) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	s.size += len(name) + len(value) + 3
	if s.size > headerLimit {
		s.tooBig, s.headers = true, nil
		return milter.RespContinue, nil
	}
	s.headers = append(s.headers, field{name, value})
	return milter.RespContinue, nil
}

// Headers chooses the key and starts the signer with the header block.
// What cannot be signed is deferred: the socket is attached with
// default_action=tempfail, and so is every refusal here.
//
// h is not read, and is empty (see senderSession.Headers).
func (s *signSession) Headers(h textproto.MIMEHeader, m *milter.Modifier) (milter.Response, error) {
	if s.tooBig {
		log.Printf("dkim-signer: login %q: a header section of more than %d bytes - refused", s.login, headerLimit)
		return milter.NewResponseStr('y', fmt.Sprintf("552 5.3.4 The header section is larger than %d bytes", headerLimit)), nil
	}
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
	if name := longHeaderLine(s.headers); name != "" {
		log.Printf("dkim-signer: %s (login %q): a line of the %s header is longer than %d bytes - Postfix breaks it on delivery and the signature will not verify", s.domain, s.login, name, smtpLineLimit)
	}
	keys, block := signLines(s.headers)
	s.headers = nil // the block is what is kept now
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
// in signHeaders, in message order; then each of overSign the message has,
// once more, in the order it first comes; then From once more (RFC 6376
// § 5.4.2; OversignHeaders From in OpenDKIM's configuration until 2.8.0;
// go-msgauth v0.7.0, dkim/sign.go, NewSigner(): a name with no instance left
// is kept in h= and hashes nothing).
func signLines(headers []field) ([]string, []byte) {
	var keys, again []string
	seen := map[string]bool{}
	var b strings.Builder
	for _, f := range headers {
		b.WriteString(f.name)
		b.WriteString(":")
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(f.value, "\r\n", "\n"), "\n", "\r\n"))
		b.WriteString("\r\n")
		name := strings.TrimSpace(f.name)
		lower := strings.ToLower(name)
		if !signHeaders[lower] {
			continue
		}
		keys = append(keys, name)
		if overSign[lower] && !seen[lower] {
			seen[lower] = true
			again = append(again, name)
		}
	}
	b.WriteString("\r\n")
	keys = append(keys, again...)
	return append(keys, "From"), []byte(b.String())
}

// longHeaderLine names the first header with a line longer than
// smtpLineLimit, "" when none has: Postfix's SMTP client breaks such a line
// on the way out, after it was signed whole, as it breaks a body line
// (security audit 2026-10-17, M8: only body lines were logged). A header's
// first line is "Name:" and the value with its leading space; its folded
// lines come as "\n"-separated parts of the value (Header).
func longHeaderLine(headers []field) string {
	for _, h := range headers {
		lines := strings.Split(h.value, "\n")
		if len(h.name)+1+len(lines[0]) > smtpLineLimit {
			return h.name
		}
		for _, l := range lines[1:] {
			if len(strings.TrimRight(l, "\r")) > smtpLineLimit {
				return h.name
			}
		}
	}
	return ""
}

// BodyChunk hashes the body as it comes. Postfix 3.10.13,
// src/milter/milter8.c:2470-2500, milter8_body(): every line ends "\r\n".
// A line longer than smtpLineLimit is logged, once a message: its signature
// will not verify where the line is broken on the way out - the panel's log
// names the domain for whoever has to tell the application's author.
func (s *signSession) BodyChunk(chunk []byte, m *milter.Modifier) (milter.Response, error) {
	if s.signer == nil {
		return milter.RespContinue, nil
	}
	if !s.long {
		for _, c := range chunk {
			if c == '\n' {
				s.line = 0
				continue
			}
			if c != '\r' {
				s.line++
			}
			if s.line > smtpLineLimit {
				s.long = true
				log.Printf("dkim-signer: %s (login %q): a body line longer than %d bytes - Postfix breaks it on delivery and the signature will not verify", s.domain, s.login, smtpLineLimit)
				break
			}
		}
	}
	if _, err := s.signer.Write(chunk); err != nil {
		log.Printf("dkim-signer: %s: %v - deferring", s.domain, err)
		s.reset()
		return milter.RespTempFail, nil
	}
	return milter.RespContinue, nil
}

// Body is the end of the message: the signature goes in at the top, where
// OpenDKIM put it until 2.8.0 (OpenDKIM 2.11.0~beta2, opendkim/opendkim.c:3673,
// dkimf_insheader(ctx, 0, ...)).
// Postfix 3.10.13, src/milter/milter8.c:1445-1465, SMFIR_INSHEADER: the
// index is passed on plus one; src/cleanup/cleanup_milter.c:1193,
// cleanup_ins_header(): "Index 1 is the top-most header" - above Postfix's
// own Received:, which a milter never sees (milter8.c:2352-2358, "insert
// requests are relative to the message as delivered"). go-milter v0.4.1,
// modifier.go, InsertHeader() turns the folded value's CRLF into LF, as
// Postfix keeps a header. The value carries its own leading space: a milter
// that asked for SMFIP_HDR_LEADSPC gets none from Postfix
// (milter8.c:1378, :1464, MILTER8_HDR_SPACE; cleanup_milter.c:1085,
// "%s:%s%s").
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
	if err := m.InsertHeader(0, name, " "+strings.TrimSuffix(value, "\r\n")); err != nil {
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
