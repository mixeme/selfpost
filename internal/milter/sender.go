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
// one table, read the way Postfix reads it and looked up the way Postfix looks
// it up (Bindings). The send-log row of a refusal is written by another
// goroutine and is not waited for.
//
// Postfix 3.7.11, src/smtpd/smtpd_check.c, reject_auth_sender_login_mismatch():
// the SASL login is compared with an owner by strcasecmp_utf8();
// src/milter/milter8.c, milter8_comm_error(): default_action=tempfail answers
// "451 4.7.1" for a milter that does not answer. OpenDKIM 2.11.0~beta2,
// opendkim/opendkim.c, mlfi_eoh(): with SenderHeaders unset the signing
// domain is that of the first From: field, as dkim_mail_parse() reads it.

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
	mu       sync.Mutex
	file     tableFile
	logins   map[string]map[string]bool // sender -> logins, both in lower case
	domainOf map[string]string          // login -> the domain it sends as
	broken   error                      // why Postfix would not use the table
}

// NewBindings reads the file at path on first use.
func NewBindings(path string) *Bindings {
	return &Bindings{file: tableFile{path: path}}
}

// fresh makes the table match the file.
func (b *Bindings) fresh() error {
	lines, again, err := b.file.changed()
	if err != nil {
		return err
	}
	if again {
		b.logins, b.domainOf, b.broken = parseSenderMap(lines)
	}
	return b.broken
}

// parseSenderMap reads the lines of a sender_login_maps file as Postfix 3.7.11
// reads a texthash table (src/util/dict_thash.c, dict_thash_open(), over
// src/util/readlline.c, readllines()):
//   - a line that starts with a blank continues the one before; an empty
//     line, or one whose first non-blank is "#", is skipped;
//   - the key ends at the first blank outside double quotes, a backslash
//     escaping the next character; a line with an unbalanced quote, or
//     without a key or a value, is ignored (with a warning, in Postfix);
//   - the key is folded to lower case (DICT_FLAG_FOLD_FIX, which
//     src/smtpd/smtpd_check.c, pre_jail_init(), gives
//     smtpd_sender_login_maps);
//   - a key given twice makes the whole table unusable: no DICT_FLAG_DUP_*
//     flag is given, so dict_thash_open() returns a surrogate that fails
//     every lookup, and smtpd answers 451 (reject_server_error()). The check
//     then defers mail as well.
//
// The owners are split on commas and blanks (smtpd_check.c,
// reject_auth_sender_login_mismatch(), mystrtok(CHARS_COMMA_SP)).
func parseSenderMap(lines []string) (logins map[string]map[string]bool, domains map[string]string, broken error) {
	var logical []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case trimmed == "" || trimmed[0] == '#':
			continue
		case len(trimmed) < len(line) && len(logical) > 0:
			logical[len(logical)-1] += line
		default:
			logical = append(logical, trimmed)
		}
	}
	logins = make(map[string]map[string]bool)
	domains = make(map[string]string)
	for _, line := range logical {
		key, value, ok := splitTableLine(line)
		if !ok {
			continue
		}
		sender := strings.ToLower(key)
		if _, dup := logins[sender]; dup {
			return nil, nil, fmt.Errorf("sender map: duplicate entry %q, which Postfix refuses the whole table for", sender)
		}
		set := make(map[string]bool)
		logins[sender] = set
		for _, l := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
		}) {
			l = strings.ToLower(l)
			set[l] = true
			if _, known := domains[l]; !known {
				domains[l] = domainOf(sender)
			}
		}
	}
	return logins, domains, nil
}

// splitTableLine splits a logical texthash line into its key and its value,
// as dict_thash_open() does: the key ends at the first blank outside double
// quotes (a backslash escaping the character after it), and the value is the
// rest without the blanks around it. ok is false where Postfix ignores the
// line: an unbalanced quote, no key, or no value.
func splitTableLine(line string) (key, value string, ok bool) {
	inQuotes := false
	i := 0
	for ; i < len(line); i++ {
		c := line[i]
		if c == '\\' {
			i++
			continue
		}
		if c == '"' {
			inQuotes = !inQuotes
			continue
		}
		if (c == ' ' || c == '\t') && !inQuotes {
			break
		}
	}
	if inQuotes {
		return "", "", false
	}
	if i > len(line) {
		i = len(line)
	}
	key, value = line[:i], strings.Trim(line[i:], " \t")
	return key, value, key != "" && value != ""
}

// Allowed reports whether login may send as addr - the lookup Postfix makes
// for the envelope sender. Postfix 3.7.11, src/smtpd/smtpd_check.c,
// reject_auth_sender_login_mismatch(), takes the owners of the first key
// mail_addr_find() finds (src/global/mail_addr_find.c, mail_addr_find_opt()
// with MA_FIND_DEFAULT): the address, then the address without its
// "+extension", then "@domain" - each tried only when the one before is not
// in the table - and compares the login with them by strcasecmp_utf8(). So
// an address bound to one application is not open to another that holds its
// whole domain (assumptions B-PS1). The bare local part MA_FIND_DEFAULT also
// tries for a local domain is never a key here: every key the panel writes
// has an "@" (internal/postfix renderSenderLoginMaps).
func (b *Bindings) Allowed(login, addr string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fresh(); err != nil {
		return false, err
	}
	login, addr = strings.ToLower(login), strings.ToLower(addr)
	keys := []string{addr}
	// recipient_delimiter is "+" (build/postfix-config.sh), so an
	// application listed as invoices@ may also send as invoices+march@.
	if bare := withoutExtension(addr); bare != addr {
		keys = append(keys, bare)
	}
	if d := domainOf(addr); d != "" {
		keys = append(keys, "@"+d)
	}
	for _, k := range keys {
		if owners, found := b.logins[k]; found {
			return owners[login], nil
		}
	}
	return false, nil
}

// extensionDelimiter is Postfix's recipient_delimiter, which
// build/postfix-config.sh sets to this.
const extensionDelimiter = "+"

// withoutExtension drops the extension of an address's local part: what
// follows its first delimiter. "user+tag@example.org" is "user@example.org";
// an address whose local part begins with the delimiter has no extension.
//
// Postfix 3.7.11, src/global/strip_addr.c, strip_addr_internal(), and
// split_addr.c, split_addr_internal(): the local part, up to the last "@", is
// cut at its first delimiter, unless that is its first character.
func withoutExtension(addr string) string {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return addr
	}
	if i := strings.Index(addr[:at], extensionDelimiter); i > 0 {
		return addr[:i] + addr[at:]
	}
	return addr
}

// DomainOf returns the domain login sends as, from the first of its bindings
// in the file's order; "" for a login that has none. An application belongs
// to one domain, so all of its bindings name the same one.
func (b *Bindings) DomainOf(login string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fresh() != nil {
		return ""
	}
	return b.domainOf[strings.ToLower(login)]
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
//
// OpenDKIM 2.11.0~beta2, opendkim/opendkim-db.c, dkimf_db_open(), case
// DKIMF_DB_TYPE_REFILE: the pattern is a line up to its first blank, "#"
// starts a comment; opendkim/util.c, dkimf_mkregexp(): "^" + the pattern
// ("*" as ".*", "." escaped) + "$", compiled REG_ICASE (DKIMF_DB_FLAG_ICASE,
// opendkim.c). So "*@d" signs user@d in any case, and no subdomain of d.
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
//
// go-milter v0.4.1, session.go, HandleMilterCommands(): a new backend after a
// reply whose Continue() is false - the tempfail of Headers; the "y" reply is
// a CustomResponse whose Continue() is true (response.go), so it keeps this
// one. {auth_authen}: see session.MailFrom in milter.go.
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
//
// go-milter v0.4.1, session.go, case CodeEOH: Headers(). Postfix 3.7.11,
// src/milter/milter8.c, milter8_event(), SMFIR_REPLYCODE: a "4"/"5" reply
// whose enhanced code starts with the same digit is the message's reply;
// src/cleanup/cleanup_milter.c, cleanup_milter_apply(): a "5" rejects the
// message, and src/smtpd/smtpd.c, common_post_message_handling(), says that
// reply to the client.
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
	// cleanup has given the message a queue id by the end of its headers
	// (milter_end_of_header_macros has {i}); Postfix's lines about it - the
	// refusal among them - are found by it on the message's page.
	// Postfix 3.7.11, src/global/mail_params.h: DEF_MILT_EOH_MACROS "i",
	// answered by src/cleanup/cleanup_milter.c, cleanup_milter_eval().
	s.record(shown, macro(m, "i"))
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
		// Postfix 3.7.11, src/smtpd/smtpd_check.c, smtpd_check_rcpt(): with
		// smtpd_delay_reject (default yes) smtpd_client_restrictions
		// (permit_sasl_authenticated,reject on 465 and 587) refuse every RCPT;
		// src/smtpd/smtpd.c, data_cmd(): no valid recipients, no message.
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
			// Which one is not said: the reply would confirm to whoever
			// tries names that another tenant sends from this server (audit
			// 2026-10-12, L8). The line as written is in the reply and on
			// the refused message's page.
			return printable(raw), "it names another sender of this server", nil
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
func (s *senderSession) record(shown, queueID string) {
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
		QueueID:    queueID,
		Domain:     s.domain(),
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

// domain is the sending domain of the refused message for its row in the
// log: the envelope sender's, which Postfix has held against the login - or,
// for the null sender of a bounce an application writes itself, which Postfix
// holds against nobody, the domain the login is bound to. Without it the row
// would belong to no domain and its administrator would never see it.
//
// Postfix 3.7.11, src/smtpd/smtpd_check.c, generic_checks():
// reject_*_sender_login_mismatch run only "if (state->sender &&
// *state->sender)".
func (s *senderSession) domain() string {
	if d := domainOf(s.from); d != "" {
		return d
	}
	return s.bind.DomainOf(s.login)
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
//
// Postfix 3.7.11, src/milter/milter8.c, milter8_event(), SMFIR_REPLYCODE:
// "%%" becomes "%" and a single "%" is dropped; src/cleanup/cleanup_milter.c,
// cleanup_milter_apply(), logs "<id>: milter-reject: END-OF-MESSAGE from
// <client>: <text>; from=<...> to=<...> ...".
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
		case r == ';':
			// Postfix logs the reply and then "; from=<...> to=<...>" of
			// its own; a ";" of the client's would let its text pass for
			// Postfix's where the log tailer reads that line.
			sb.WriteByte(',')
		default:
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}

// ServeSender runs the sender check on ln until ctx is cancelled. rec may be
// nil: refusals are then not logged. What it relies on of go-milter and
// Postfix is cited at Serve in milter.go.
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
