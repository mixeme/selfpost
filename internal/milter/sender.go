package milter

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-milter"

	"github.com/mixeme/selfpost/internal/mailhdr"
	"github.com/mixeme/selfpost/internal/store"
)

// The sender check: a message's From: line is held against the application
// that sends it, under the rule Postfix holds the envelope sender to
// (docs/plans/from-header-binding.md). OpenDKIM chooses the signing key by
// that line, so without the check an application could have mail signed as
// another domain of the server, or as the server itself.
//
// Unlike the journal milter this one is attached strictly
// (default_action=tempfail in build/postfix-config.sh): while it does not
// answer, Postfix defers mail. It therefore depends on as little as it can.
// It reads no database: what an application may send as comes from the file
// of sender bindings Postfix itself reads for the envelope, so the two go by
// one table. Where the lookups differ the check is the stricter: Postfix
// also tries an address without its "+extension" and compares logins without
// regard to case. The send-log row of a refusal is written by another goroutine and
// is not waited for.

// tableFile is a table of the mail path that the panel rewrites and the check
// reads: it is read again when it has changed. An unreadable file is an error
// and the check then defers mail - a table from before would let a deleted
// application go on sending, and no table would refuse everyone for good.
type tableFile struct {
	path    string
	modTime time.Time
	size    int64
	loaded  bool
}

// changed returns the file's lines when they have to be read (again), and
// nil when what was read last still stands.
func (f *tableFile) changed() (lines []string, again bool, err error) {
	fi, err := os.Stat(f.path)
	if err != nil {
		return nil, false, err
	}
	if f.loaded && fi.ModTime().Equal(f.modTime) && fi.Size() == f.size {
		return nil, false, nil
	}
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return nil, false, err
	}
	f.modTime, f.size, f.loaded = fi.ModTime(), fi.Size(), true
	return strings.Split(string(raw), "\n"), true, nil
}

// Bindings is Postfix's sender_login_maps file as the check reads it: each
// sender - an address, or "@domain" for any address of a domain - with the
// logins that may use it.
type Bindings struct {
	mu     sync.Mutex
	file   tableFile
	logins map[string]map[string]bool // sender -> logins
}

// NewBindings reads the file at path on first use.
func NewBindings(path string) *Bindings {
	return &Bindings{file: tableFile{path: path}}
}

// fresh makes the table match the file.
func (b *Bindings) fresh() error {
	lines, again, err := b.file.changed()
	if err != nil || !again {
		return err
	}
	logins := make(map[string]map[string]bool)
	for _, line := range lines {
		// texthash: <key><whitespace><value>, the value a comma-separated
		// list of logins (internal/postfix renderSenderLoginMaps).
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		sender := strings.ToLower(fields[0])
		set := logins[sender]
		if set == nil {
			set = make(map[string]bool)
			logins[sender] = set
		}
		for _, l := range strings.Split(strings.Join(fields[1:], ""), ",") {
			if l != "" {
				set[l] = true
			}
		}
	}
	b.logins = logins
	return nil
}

// Allowed reports whether login may send as addr: the address itself is bound
// to it, or its whole domain is. This is the lookup Postfix makes for the
// envelope sender.
func (b *Bindings) Allowed(login, addr string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fresh(); err != nil {
		return false, err
	}
	addr = strings.ToLower(addr)
	if b.logins[addr][login] {
		return true, nil
	}
	if d := domainOf(addr); d != "" && b.logins["@"+d][login] {
		return true, nil
	}
	return false, nil
}

// Signers is OpenDKIM's SigningTable as the check reads it: the names this
// server signs for - every sending domain, with applications or without, and
// the server's own name.
type Signers struct {
	mu    sync.Mutex
	file  tableFile
	names map[string]bool
}

// NewSigners reads the file at path on first use.
func NewSigners(path string) *Signers {
	return &Signers{file: tableFile{path: path}}
}

// Signs reports whether mail whose From: is at domain gets a signature.
func (s *Signers) Signs(domain string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, again, err := s.file.changed()
	if err != nil {
		return false, err
	}
	if again {
		names := make(map[string]bool)
		for _, line := range lines {
			// "*@<name> <key name>" (internal/domain renderTables).
			fields := strings.Fields(line)
			if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			if d := domainOf(fields[0]); d != "" {
				names[d] = true
			}
		}
		s.names = names
	}
	return s.names[strings.ToLower(domain)], nil
}

// Refusals is where the check leaves the send-log rows of what it refused.
// *store.Store satisfies it.
type Refusals interface {
	InsertRejected(e store.SendLogEntry) error
}

// senderSession is one connection of the sender check.
type senderSession struct {
	milter.NoOpMilter
	bind    *Bindings
	signers *Signers
	refused chan<- store.SendLogEntry

	login   string
	from    string
	rcpts   []string
	subject string
	froms   []string // the value of every From: field of the message
}

// MailFrom starts a message and forgets the one before it: a connection may
// carry several, and after a temporary failure the library hands the
// connection a new session.
func (s *senderSession) MailFrom(from string, m *milter.Modifier) (milter.Response, error) {
	s.from = cleanAddress(from)
	s.login = macro(m, "auth_authen")
	s.rcpts, s.subject, s.froms = nil, "", nil
	return milter.RespContinue, nil
}

func (s *senderSession) RcptTo(rcpt string, m *milter.Modifier) (milter.Response, error) {
	s.rcpts = append(s.rcpts, cleanAddress(rcpt))
	return milter.RespContinue, nil
}

func (s *senderSession) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	// The name is trimmed: the obsolete syntax allows blanks before the colon.
	switch textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name)) {
	case "From":
		s.froms = append(s.froms, value)
	case "Subject":
		if s.subject == "" {
			s.subject = mailhdr.DecodeSubject(value)
		}
	}
	return milter.RespContinue, nil
}

// Headers is the end of the headers, where the message is judged.
func (s *senderSession) Headers(h textproto.MIMEHeader, m *milter.Modifier) (milter.Response, error) {
	shown, why, err := s.judge()
	if err != nil {
		// Strict: what cannot be checked is not let through.
		log.Printf("sender-check: %v - deferring mail of %q", err, s.login)
		return milter.RespTempFail, nil
	}
	if why == "" {
		return milter.RespContinue, nil
	}
	log.Printf("sender-check: application %q refused: %s (From: %s)", s.login, why, shown)
	s.record(shown)
	// A reply code of its own, so that the application is told what is wrong
	// and, the refusal being permanent, does not send the message again.
	return milter.NewResponseStr('y', "550 5.7.1 From: "+shown+" - "+why), nil
}

func (s *senderSession) Abort(m *milter.Modifier) error {
	s.rcpts, s.subject, s.froms = nil, "", nil
	return nil
}

// judge decides about the message at hand. why is empty when it may pass;
// otherwise it says what is wrong, and shown is the From: line in a form that
// is safe to print - both go into the reply to the client.
func (s *senderSession) judge() (shown, why string, err error) {
	if s.login == "" {
		// Not reachable while the submission ports demand authentication; a
		// message nobody answers for is not signed either way.
		return "(none)", "no authenticated application", nil
	}
	switch len(s.froms) {
	case 0:
		return "(none)", "the message has no From: line", nil
	case 1:
	default:
		return printable(strings.Join(s.froms, " | ")), "the message has more than one From: line", nil
	}
	// Postfix hands a milter a header as it came, folded lines and all; the
	// address parser reads one line.
	raw := unfold(s.froms[0])
	shown = printable(raw)
	list, perr := fromParser.ParseList(raw)
	if perr != nil || len(list) != 1 || list[0].Address == "" {
		return shown, "it must be one address", nil
	}
	addr := strings.ToLower(list[0].Address)
	shown = printable(addr)
	ok, err := s.bind.Allowed(s.login, addr)
	if err != nil {
		return shown, "", fmt.Errorf("sender bindings: %w", err)
	}
	if !ok {
		return shown, "not an address this application sends as", nil
	}
	// A cruder second look. OpenDKIM finds the address in this line with a
	// parser of its own, and the key it signs with follows what that parser
	// finds. Wherever the two could read the line differently - a display
	// name, a comment, a quoted string - the other address would have to be
	// spelled out in it, so no other name this server signs for may appear.
	//
	// The display name is looked at once more as a reader will see it: a mail
	// program decodes an encoded word, and "=?utf-8?q?director=40example.org?="
	// shows as an address of another sender though no "@" is written in it.
	own := domainOf(addr)
	for _, d := range append(mentionedDomains(raw), mentionedDomains(list[0].Name)...) {
		if d == own {
			continue
		}
		signs, err := s.signers.Signs(d)
		if err != nil {
			return shown, "", fmt.Errorf("signing table: %w", err)
		}
		if signs {
			return printable(raw), "it names another sender of this server, " + d, nil
		}
	}
	// And one that needs to know nothing of OpenDKIM's parser: outside quoted
	// strings the line has the one "@" of its address. A second one - in a
	// comment, in an unquoted name - is where two parsers could part ways.
	if strings.Count(unquoted(raw), "@") != 1 {
		return printable(raw), "outside quotes it must have one @, the address's own", nil
	}
	return shown, "", nil
}

// record leaves a row for the Outbound log. It never waits: a database that
// is slow must not make the check slow. One row for the message, not one for
// each recipient as an accepted message has: nothing was sent to anybody, and
// a refused message with a thousand recipients would otherwise be a thousand
// rows at the price of one.
func (s *senderSession) record(shown string) {
	if s.refused == nil {
		return
	}
	to := ""
	if len(s.rcpts) > 0 {
		to = s.rcpts[0]
	}
	if more := len(s.rcpts) - 1; more > 0 {
		to += fmt.Sprintf(" and %d more", more)
	}
	e := store.SendLogEntry{
		Domain:     domainOf(s.from),
		AppLogin:   s.login,
		From:       s.from,
		To:         to,
		Subject:    s.subject,
		Reason:     store.ReasonFrom,
		HeaderFrom: shown,
	}
	select {
	case s.refused <- e:
	default:
		log.Printf("sender-check: the refusal of %s -> %s is not in the log: too many are waiting to be written", s.from, to)
	}
}

// fromParser reads the From: line. A display name encoded in a charset Go
// does not convert (windows-1251, koi8-r, ...) is still a display name: its
// bytes are passed through unread, since only the address is judged.
var fromParser = mail.AddressParser{WordDecoder: &mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) { return input, nil },
}}

// unfold joins the lines of a folded header value (RFC 5322 2.2.3): a line
// break is dropped, the white space that follows it stays.
func unfold(v string) string {
	return strings.NewReplacer("\r\n", "", "\n", "", "\r", "").Replace(v)
}

// unquoted returns v without its quoted strings, a backslash inside one
// escaping the next character.
func unquoted(v string) string {
	var sb strings.Builder
	in := false
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case in && c == '\\':
			i++
		case c == '"':
			in = !in
		case !in:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// mentionedDomains returns, lower-cased, what follows every "@" of v as far
// as it reads as a host name.
func mentionedDomains(v string) []string {
	var out []string
	for i := 0; i < len(v); i++ {
		if v[i] != '@' {
			continue
		}
		j := i + 1
		for j < len(v) && isHostByte(v[j]) {
			j++
		}
		if d := strings.Trim(strings.ToLower(v[i+1:j]), ".-"); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func isHostByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-'
}

// printable reduces what a client wrote to printable ASCII without the
// characters that mean something in an SMTP reply or a log line, and cuts it:
// it is said back to the client, logged and stored.
func printable(v string) string {
	const max = 120
	var sb strings.Builder
	for _, r := range v {
		if sb.Len() >= max {
			sb.WriteString("...")
			break
		}
		switch {
		case r == '%' || r < 0x20 || r > 0x7e:
			sb.WriteByte('?')
		default:
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}

// ServeSender runs the sender check on ln until ctx is cancelled. rec may be
// nil: refusals are then not logged.
func ServeSender(ctx context.Context, ln net.Listener, bind *Bindings, signers *Signers, rec Refusals) error {
	var refused chan store.SendLogEntry
	if rec != nil {
		refused = make(chan store.SendLogEntry, 256)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case e := <-refused:
					if err := rec.InsertRejected(e); err != nil {
						log.Printf("sender-check: record refused %s: %v", e.From, err)
					}
				}
			}
		}()
	}
	srv := &milter.Server{
		NewMilter: func() milter.Milter {
			return &senderSession{bind: bind, signers: signers, refused: refused}
		},
		Actions:  0,                // read-only
		Protocol: milter.OptNoBody, // the headers decide
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
