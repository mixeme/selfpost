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
// (docs/plans/from-header-binding.md). The signer signs with the key of the
// domain the login belongs to (sign.go), and a receiver holds that signature
// to the domain of this line (DMARC, RFC 7489 § 3.1.1), so without the check
// an application could send under the server's signature as another domain
// of the server, or as the server itself.
//
// Unlike the journal milter this one is attached strictly
// (default_action=tempfail in build/postfix-config.sh): while it does not
// answer, Postfix answers the client with a temporary error. It therefore
// depends on as little as it can.
// It reads no database: what an application may send as comes from the file
// of sender bindings Postfix itself reads for the envelope, so the two go by
// one table, read the way Postfix reads it and looked up the way Postfix looks
// it up (Bindings). The send-log row of a refusal is written by another
// goroutine and is not waited for.
//
// Postfix 3.10.13, src/smtpd/smtpd_check.c, reject_auth_sender_login_mismatch():
// the SASL login is compared with an owner by strcasecmp_utf8();
// src/milter/milter8.c, milter8_comm_error(): default_action=tempfail answers
// "451 4.7.1" for a milter that does not answer.

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

// parseSenderMap reads the lines of a sender_login_maps file as Postfix 3.10.13
// reads a texthash table (src/util/dict_thash.c, dict_thash_open(), over
// src/util/readlline.c, readllines()):
//   - a line that starts with a blank continues the one before; an empty
//     line, or one whose first non-blank is "#", is skipped;
//   - the key ends at the first blank outside double quotes, a backslash
//     escaping the next character; a line with an unbalanced quote, or
//     without a key or a value, is ignored (with a warning, in Postfix);
//   - the key is folded to lower case (DICT_FLAG_FOLD_FIX, which
//     src/smtpd/smtpd_check.c, smtpd_check_init(), called from
//     src/smtpd/smtpd.c, pre_jail_init(), gives smtpd_sender_login_maps);
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
// for the envelope sender. Postfix 3.10.13, src/smtpd/smtpd_check.c,
// reject_auth_sender_login_mismatch(), takes the owners of the first key
// mail_addr_find() finds (src/global/mail_addr_find.c, mail_addr_find_opt()
// with MA_FIND_DEFAULT): the address, then the address without its
// "+extension", then "@domain" - each tried only when the one before is not
// in the table - and compares the login with them by strcasecmp_utf8(). So
// an address bound to one application is not open to another that holds its
// whole domain (assumptions B-PS1). The bare local part MA_FIND_DEFAULT also
// tries for a local domain is never a key here: every key the panel writes
// has an "@" (internal/postfix renderSenderLoginMaps).
//
// Case is folded in ASCII only, as Postfix folds the keys of the texthash
// map the panel writes (src/util/dict_thash.c:113 and dict_ht.c:58-64,
// lowercase(): ASCII; security audit 2026-10-17 corrected this comment, which
// said Unicode). Every key and login the panel writes is ASCII; a Unicode fold
// would only let a look-alike pass - the Kelvin sign for "k" - for an address
// that is not the domain's (review of stage 2c).
func (b *Bindings) Allowed(login, addr string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fresh(); err != nil {
		return false, err
	}
	login, addr = asciiLower(login), asciiLower(addr)
	keys := []string{addr}
	// recipient_delimiter is "+" (build/postfix-config.sh), so an
	// application listed as invoices@ may also send as invoices+march@.
	if bare := withoutExtension(addr); bare != addr {
		keys = append(keys, bare)
	}
	// The domain as written, folded in ASCII alone: domainOf folds as
	// Unicode, which would make the Kelvin sign a "k".
	if at := strings.LastIndexByte(addr, '@'); at >= 0 && at < len(addr)-1 {
		keys = append(keys, addr[at:])
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
// Postfix 3.10.13, src/global/strip_addr.c, strip_addr_internal(), and
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
	return b.domainOf[asciiLower(login)]
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
	refused chan<- store.SendLogEntry

	login   string
	from    string
	rcpts   []string
	subject string
	froms   []string // the value of every From: field of the message
	senders []string // and of every Sender: field
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
	s.rcpts, s.subject, s.froms, s.senders = nil, "", nil, nil
	return milter.RespContinue, nil
}

func (s *senderSession) RcptTo(rcpt string, m *milter.Modifier) (milter.Response, error) {
	s.rcpts = append(s.rcpts, cleanAddress(rcpt))
	return milter.RespContinue, nil
}

func (s *senderSession) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	// The name is trimmed: the obsolete syntax allows blanks before the colon.
	// Two of each are kept: the judgement needs only none, one or more, and
	// Postfix bounds a header's length but not their number (audit
	// 2026-10-17, M5: a hundred 60 000-byte From: lines were 6 MB held).
	switch textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name)) {
	case "From":
		if len(s.froms) < 2 {
			s.froms = append(s.froms, value)
		}
	case "Sender":
		if len(s.senders) < 2 {
			s.senders = append(s.senders, value)
		}
	case "Subject":
		if s.subject == "" {
			s.subject = mailhdr.DecodeSubject(value)
		}
	}
	return milter.RespContinue, nil
}

// Headers is the end of the headers, where the message is judged.
//
// go-milter v0.4.1, session.go, case CodeEOH: Headers(). Postfix 3.10.13,
// src/milter/milter8.c, milter8_event(), SMFIR_REPLYCODE: a "4"/"5" reply
// whose enhanced code starts with the same digit is the message's reply;
// src/cleanup/cleanup_milter.c, cleanup_milter_apply(): a "5" rejects the
// message, and src/smtpd/smtpd.c, common_post_message_handling(), says that
// reply to the client.
//
// h is not read, and is empty: upstream go-milter v0.4.1 added every header
// of the connection to it and emptied it only on ABORT, so on a second
// message it could still hold the first one's From: (audit 2026-10-12, L9);
// the copy SelfPost builds keeps none (third_party/go-milter/PATCHES.md). The
// session keeps its own, emptied at each MAIL FROM.
func (s *senderSession) Headers(h textproto.MIMEHeader, m *milter.Modifier) (milter.Response, error) {
	field, shown, why, err := s.judge()
	if err != nil {
		// Strict: what cannot be checked is not let through.
		log.Printf("sender-check: %v - deferring mail of %q", err, s.login)
		return milter.RespTempFail, nil
	}
	if why == "" {
		return milter.RespContinue, nil
	}
	log.Printf("sender-check: application %q refused: %s (%s: %s)", s.login, why, field, shown)
	// cleanup has given the message a queue id by the end of its headers
	// (milter_end_of_header_macros has {i}); Postfix's lines about it - the
	// refusal among them - are found by it on the message's page.
	// Postfix 3.10.13, src/global/mail_params.h: DEF_MILT_EOH_MACROS "i",
	// answered by src/cleanup/cleanup_milter.c, cleanup_milter_eval().
	reason := store.ReasonFrom
	if field == "Sender" {
		reason = store.ReasonSender
	}
	s.record(reason, shown, macro(m, "i"))
	// A reply code of its own, so that the application is told what is wrong
	// and, the refusal being permanent, does not send the message again.
	return milter.NewResponseStr('y', "550 5.7.1 "+field+": "+shown+" - "+why), nil
}

func (s *senderSession) Abort(m *milter.Modifier) error {
	s.rcpts, s.subject, s.froms, s.senders = nil, "", nil, nil
	return nil
}

// judge decides about the message at hand. why is empty when it may pass;
// otherwise it says what is wrong, field is the header it is about ("From"
// or "Sender"), and shown is that line in a form that is safe to print - all
// go into the reply to the client.
//
// A Sender: line - the agent that sent a message written by the From:
// line's author (RFC 5322 § 3.6.2), which a mail program shows beside From:
// as "on behalf of" - is held to the same rule as From: when the message has
// one, and at most one is allowed (§ 3.6, its table). The panel's signer
// signs it (sign.go), so an address of another sender there would be shown
// under this server's signature (security review of panel-dkim-signing, F1;
// the owner's decision, 2026-10-16).
func (s *senderSession) judge() (field, shown, why string, err error) {
	field = "From"
	if s.login == "" {
		// Not reachable while the submission ports demand authentication; a
		// message nobody answers for is not signed either way.
		// Postfix 3.10.13, src/smtpd/smtpd_check.c, smtpd_check_rcpt(): with
		// smtpd_delay_reject (default yes) smtpd_client_restrictions
		// (permit_sasl_authenticated,reject on 465 and 587) refuse every RCPT;
		// src/smtpd/smtpd.c, data_cmd(): no valid recipients, no message.
		return field, "(none)", "no authenticated application", nil
	}
	switch len(s.froms) {
	case 0:
		return field, "(none)", "the message has no From: line", nil
	case 1:
	default:
		return field, printable(strings.Join(s.froms, " | ")), "the message has more than one From: line", nil
	}
	if shown, why, err = s.judgeLine(s.froms[0]); err != nil || why != "" {
		return field, shown, why, err
	}
	field = "Sender"
	switch len(s.senders) {
	case 0:
		return field, "", "", nil
	case 1:
	default:
		return field, printable(strings.Join(s.senders, " | ")), "the message has more than one Sender: line", nil
	}
	shown, why, err = s.judgeLine(s.senders[0])
	return field, shown, why, err
}

// judgeLine holds one From: or Sender: line against the application.
func (s *senderSession) judgeLine(raw string) (shown, why string, err error) {
	// The line must be one address in RFC 5322's grammar (net/mail), the
	// address a mail program shows; a display name may say anything. A line
	// a lenient program could show as another address - "ceo@bank.com
	// <me@mine.com>", "ceo@bank.com:me@mine.com", "<b@evil.com, a@mine.com>"
	// - is refused (review of stage 2c, F1). The value comes as the client
	// wrote it, folded lines and all (assumptions B-PM12). And that address,
	// its local part unquoted as Postfix holds the envelope's (Postfix
	// 3.10.13, src/global/tok822_parse.c), is the one held against the
	// bindings: "x"@mine.com is x@mine.com, which may be another
	// application's (F2).
	shown = printable(raw)
	list, perr := fromParser.ParseList(unfold(raw))
	if perr != nil || len(list) != 1 {
		return shown, "it must be one address, as a mail program reads it", nil
	}
	seen := asciiLower(list[0].Address)
	shown = printable(seen)
	ok, err := s.bind.Allowed(s.login, seen)
	if err != nil {
		return shown, "", fmt.Errorf("sender bindings: %w", err)
	}
	if !ok {
		return shown, "not an address this application sends as", nil
	}
	return shown, "", nil
}

// fromParser reads the From: line as a mail program would. A display name
// encoded in a charset Go does not convert (windows-1251, koi8-r, ...) is
// still a display name: its bytes are passed through unread.
var fromParser = mail.AddressParser{WordDecoder: &mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) { return input, nil },
}}

// unfold joins the lines of a folded header value (RFC 5322 2.2.3): a line
// break is dropped, the white space that follows it stays. Postfix hands a
// milter the line breaks as LF (assumptions B-PM12).
func unfold(v string) string {
	return strings.NewReplacer("\r\n", "", "\n", "", "\r", "").Replace(v)
}

// asciiLower lower-cases A-Z only, as Postfix's table lookups do for the
// ASCII keys the panel writes; strings.ToLower would also fold characters
// outside ASCII - the Kelvin sign to "k" - into names they are not.
func asciiLower(v string) string {
	b := []byte(v)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// record leaves a row for the Outbound log. It never waits: a database that
// is slow must not make the check slow. One row for the message, not one for
// each recipient as an accepted message has: nothing was sent to anybody, and
// a refused message with a thousand recipients would otherwise be a thousand
// rows at the price of one.
func (s *senderSession) record(reason, shown, queueID string) {
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
		Reason:     reason,
		HeaderFrom: shown,
	}
	select {
	case s.refused <- e:
	default:
		log.Printf("sender-check: the refusal of %s -> %s is not in the log: too many are waiting to be written", s.from, to)
	}
}

// domain is the sending domain of the refused message for its row in the
// log: the envelope sender's, which Postfix has held against the login - or,
// for the null sender of a bounce an application writes itself, which Postfix
// holds against nobody, the domain the login is bound to. Without it the row
// would belong to no domain and its administrator would never see it.
//
// Postfix 3.10.13, src/smtpd/smtpd_check.c, generic_checks():
// reject_*_sender_login_mismatch run only "if (state->sender &&
// *state->sender)".
func (s *senderSession) domain() string {
	if d := domainOf(s.from); d != "" {
		return d
	}
	return s.bind.DomainOf(s.login)
}

// printable reduces what a client wrote to printable ASCII without the
// characters that mean something in an SMTP reply or a log line, and cuts it:
// it is said back to the client, logged and stored.
//
// Postfix 3.10.13, src/milter/milter8.c, milter8_event(), SMFIR_REPLYCODE:
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
func ServeSender(ctx context.Context, ln net.Listener, bind *Bindings, rec Refusals) error {
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
			return &senderSession{bind: bind, refused: refused}
		},
		Actions: 0, // read-only
		// The headers decide; and each header value with its leading space,
		// as the client wrote it, the bytes the signer reads too (sign.go).
		// Postfix 3.10.13, src/milter/milter8.c: milter8_negotiate() keeps
		// the event mask a milter of any version asks for (go-milter v0.4.1
		// says version 2), and milter8_header() drops the space only for a
		// milter without SMFIP_HDR_LEADSPC.
		Protocol: milter.OptNoBody | milter.OptHeaderLeadingSpace,
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
