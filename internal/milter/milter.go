// Package milter implements the SelfPost milters: the journal-milter here, the
// sender check (sender.go) and the inbound journal (inbound.go).
//
// The journal-milter is a lightweight milter (architecture.md § Mail path)
// attached to Postfix's smtpd_milters after the sender check and the signer. On the receive path it reads the SASL login, From, recipients and
// Subject of each accepted message and records one send-log row per (queue-id,
// recipient), giving the panel a structured, filterable history that raw
// mail.log cannot.
//
// It refuses only at MAIL FROM, with a 4xx tempfail, when a level-2 rate limit
// or the application's client-address list says no (see MailFrom); every other
// callback returns Continue. A failure of this milter never blocks the relay:
// Postfix is configured with default_action=accept for this milter's socket,
// so even a crash or hang fails open (architecture.md § Mail path).
//
// Postfix 3.10.13, src/milter/milter8.c, milter8_comm_error(): a milter that
// cannot be reached, or does not answer within milter_*_timeout, gives no
// reply with default_action=accept - the message goes on.
package milter

import (
	"context"
	"log"
	"net"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-milter"

	"github.com/mixeme/selfpost/internal/mailhdr"
	"github.com/mixeme/selfpost/internal/store"
)

// Store is the persistence the milter needs on the receive path: recording
// accepted messages (architecture.md § Mail path) and, for level-2 rate
// limiting (guide § Rate limiting), looking up the configured limits and
// counting recent messages. *store.Store satisfies it; tests substitute a
// fake.
type Store interface {
	InsertQueued(e store.SendLogEntry) error
	InsertRejected(e store.SendLogEntry) error
	RateLimit(scope, ref string) (store.RateLimit, bool, error)
	CountMessages(scope, ref string, since time.Time) (int64, error)
	ApplicationByLogin(login string) (store.Application, error)
}

// session accumulates the fields of one message as the milter callbacks fire.
// Milter macros arrive per-stage and do not accumulate, so each value is
// captured at the stage that carries it (architecture.md § Mail path): SASL
// login and From at MAIL, each recipient at RCPT, Subject in the headers, and
// the queue-id at end-of-message. go-milter creates one session per
// connection; a connection may carry several messages, so per-message fields
// are reset at MailFrom (the start of every transaction).
//
// go-milter v0.4.1, session.go, Process(), case CodeMacro: every macro packet
// replaces the map; server.go, Serve(): NewMilter() for each connection.
// Postfix 3.10.13, src/milter/milter8.c, milter8_event(): a macro packet, empty
// or not, goes before every event (milter.c, milter_macro_lookup(), never
// returns null).
type session struct {
	milter.NoOpMilter
	rec Store
	// flight is shared by every session of the process; it holds the messages
	// that passed the level-2 check but are not in the send log yet. Nil is a
	// valid zero value (no in-flight accounting).
	flight *inflight

	// clientIP is the client's address. Postfix gives it at Connect, once per
	// connection, and again with every MAIL FROM as the {client_addr} macro
	// (milter_mail_macros in build/postfix-config.sh). The second is the one
	// that can be relied on: see MailFrom. Postfix 3.10.13,
	// src/smtpd/smtpd_milter.c, smtpd_milter_eval(): {client_addr} is
	// state->rfc_addr (see clientAddr).
	clientIP string

	login    string
	from     string
	rcpts    []string
	subject  string
	reserved []*reservation // level-2 slots held by the current message
}

// Connect captures the client IP of a connection's first message. It is not
// enough on its own - see MailFrom.
//
// Postfix 3.10.13, src/smtpd/smtpd.c, smtpd_proto(): milter_conn_event() is
// given state->addr, the address without "IPv6:". go-milter v0.4.1,
// session.go, case CodeConn: net.ParseIP of it - nil for "unknown" and for a
// family that sends no address.
func (s *session) Connect(host, family string, port uint16, addr net.IP, m *milter.Modifier) (milter.Response, error) {
	if addr != nil {
		s.clientIP = addr.String()
	}
	return milter.RespContinue, nil
}

// MailFrom starts a new message: reset per-message state, then capture the
// envelope sender and the SASL login ({auth_authen}, carried by the MAIL-stage
// macros). This is also the earliest stage where both the sending domain (from
// the sender) and the application (the login) are known, so the level-2 rate
// limit is enforced here: over the limit, the message is refused with a 4xx
// tempfail before recipients are even offered (guide § Rate limiting).
// Enforcement is fail-open — see overLimit.
//
// The client address is read again here, from the {client_addr} macro. The
// milter library replaces the session with a new one after every reply that is
// not "continue" - a refusal, or the accept that used to end a message - and
// Postfix does not send Connect again on the same connection. A session made
// that way never saw the client, so every message of a connection after the
// first, and every MAIL FROM after a refused one, used to be judged with no
// client address: the level-2 limit and an application's client-address list
// both let it through. (Found on 2026-10-12; test/e2e sends two MAIL FROM on
// one connection.)
//
// go-milter v0.4.1, session.go, HandleMilterCommands(): after a reply whose
// Continue() is false (response.go: accept, discard, reject, tempfail) the
// backend is replaced by NewMilter(). Postfix 3.10.13, src/milter/milter8.c,
// milter8_conn_event(): CONNECT goes once per milter connection.
// {auth_authen}: src/smtpd/smtpd_milter.c, smtpd_milter_eval(), gives
// state->sasl_username, null without authentication, and then milter.c,
// milter_macro_lookup(), sends no such macro.
// The tempfail: milter8.c, milter8_event(), SMFIR_TEMPFAIL is "451 4.7.1";
// src/smtpd/smtpd.c, mail_cmd(), replies it and calls mail_reset() - the
// transaction ends, the connection goes on.
func (s *session) MailFrom(from string, m *milter.Modifier) (milter.Response, error) {
	s.releaseReservations() // a previous transaction that ended without EOM/ABORT
	if ip := clientAddr(m); ip != "" {
		s.clientIP = ip
	}
	s.from = cleanAddress(from)
	s.login = macro(m, "auth_authen")
	s.rcpts = nil
	s.subject = ""
	if !s.authIPAllowed() {
		s.recordRejected(store.ReasonClient)
		return milter.RespTempFail, nil
	}
	if s.overLimit() {
		s.recordRejected(store.ReasonLimit)
		return milter.RespTempFail, nil
	}
	return milter.RespContinue, nil
}

// RcptTo records each recipient. Postfix calls this once per recipient, which
// is what lets the journal keep a separate row per (queue-id, recipient).
//
// Postfix 3.10.13, src/smtpd/smtpd.c, rcpt_cmd(): milter_rcpt_event() once per
// RCPT TO; a recipient Postfix refused goes only to a milter that asked for
// SMFIP_RCPT_REJ, which Serve's Protocol does not.
// A recipient given twice - in any case - is one row: cleanup keeps the first
// and drops the rest (Postfix 3.10.13, src/cleanup/cleanup_out_recipient.c,
// cleanup_out_recipient(), been_here() on a table made with BH_FLAG_FOLD in
// cleanup_state.c), so only one delivery line will come (assumptions A-31).
// Its key also holds the DSN NOTIFY= and ORCPT= of the RCPT TO command, which
// go-milter does not hand over (session.go, case CodeRcpt): a recipient given
// twice with different ones is two recipients to Postfix and one row here.
func (s *session) RcptTo(rcpt string, m *milter.Modifier) (milter.Response, error) {
	to := recipientAsLogged(rcpt)
	for _, have := range s.rcpts {
		if strings.EqualFold(have, to) {
			return milter.RespContinue, nil
		}
	}
	s.rcpts = append(s.rcpts, to)
	return milter.RespContinue, nil
}

// Header captures the Subject. Only the first Subject header is kept.
func (s *session) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	if s.subject == "" && textproto.CanonicalMIMEHeaderKey(name) == "Subject" {
		s.subject = mailhdr.DecodeSubject(value)
	}
	return milter.RespContinue, nil
}

// Body fires at end-of-message, when the queue-id macro {i} is set and the
// message is about to be committed to the queue. This is where the "queued"
// rows are written. The reply is "continue", which at end-of-message lets the
// message through just as "accept" does - and, unlike it, leaves this session
// in place for the connection's next message (see MailFrom).
//
// go-milter v0.4.1, session.go, case CodeEOB: Body(). Postfix 3.10.13,
// src/global/mail_params.h: DEF_MILT_EOD_MACROS "i", which
// src/cleanup/cleanup_milter.c, cleanup_milter_eval(), answers with the queue
// id. src/milter/milter8.c, milter8_event(): SMFIR_CONTINUE returns the
// milter's default reply, none, and the message goes on.
func (s *session) Body(m *milter.Modifier) (milter.Response, error) {
	s.record(macro(m, "i"))
	// The rows are in the send log now, so the stored count sees this message
	// and its level-2 slots are no longer needed.
	s.releaseReservations()
	return milter.RespContinue, nil
}

// Abort ends the current transaction without an end-of-message (client RSET, or
// Postfix rejecting the message for its own reasons). No send-log row will be
// written, so the level-2 slots this message held must go back.
//
// Postfix 3.10.13, src/smtpd/smtpd.c: rset_cmd() and the refusals call
// mail_reset(), which calls milter_abort() (src/milter/milter8.c,
// milter8_abort(): SMFIC_ABORT); so do the end of every transaction and the
// end of the session in smtpd_proto().
func (s *session) Abort(m *milter.Modifier) error {
	s.releaseReservations()
	s.rcpts = nil
	s.subject = ""
	return nil
}

// macro reads a milter macro, tolerating Postfix's convention of wrapping
// multi-character macro names in curly braces (e.g. {auth_authen}) while
// single-character names (e.g. i) arrive bare. go-milter stores whatever name
// Postfix sends verbatim, so a lookup must try both forms.
//
// Postfix 3.10.13, src/milter/milter.c, milter_macro_lookup(): each name is
// sent as milter_*_macros spells it. go-milter v0.4.1, session.go, case
// CodeMacro: m.macros[name] = value, the name as received.
func macro(m *milter.Modifier, name string) string {
	if v, ok := m.Macros[name]; ok {
		return v
	}
	return m.Macros["{"+name+"}"]
}

// clientAddr is the client's address from the {client_addr} macro, or "" when
// Postfix did not send one that reads as an address. An IPv6 address may come
// in Sendmail's form, with "IPv6:" in front.
//
// Postfix 3.10.13, src/smtpd/smtpd_peer.c, smtpd_peer_sockaddr_to_hostaddr():
// rfc_addr is the dotted quad, "IPv6:" + the address for IPv6, a V4-mapped
// address rewritten to IPv4, or "unknown".
func clientAddr(m *milter.Modifier) string {
	raw := strings.TrimSpace(macro(m, "client_addr"))
	if len(raw) > 5 && strings.EqualFold(raw[:5], "IPv6:") {
		raw = raw[5:]
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return ""
	}
	return ip.String()
}

// record writes one send-log row per recipient. Failures are logged, never
// propagated: journalling must not affect mail acceptance (architecture.md §
// Mail path).
func (s *session) record(queueID string) {
	domain := domainOf(s.from)
	rcpts := s.rcpts
	if len(rcpts) == 0 {
		// No recipient seen (unusual) — still record the message so it is
		// visible in the log rather than silently dropped.
		rcpts = []string{""}
	}
	for _, to := range rcpts {
		err := s.rec.InsertQueued(store.SendLogEntry{
			QueueID:  queueID,
			Domain:   domain,
			AppLogin: s.login,
			From:     s.from,
			To:       to,
			Subject:  s.subject,
		})
		if err != nil {
			log.Printf("journal-milter: record %s -> %s: %v", queueID, to, err)
		}
	}
}

// cleanAddress is an address as the milter gets it, without the angle
// brackets: Postfix 3.10.13 hands the milter the command's argument as the
// client wrote it, ESMTP parameters as separate strings (src/smtpd/smtpd.c,
// mail_cmd()/rcpt_cmd(), milter_argv()), and go-milter v0.4.1 passes the first
// with "<>" trimmed (session.go, case CodeMail/CodeRcpt). A quoted local part
// may hold a blank, so nothing is cut at one (assumptions B-G5).
func cleanAddress(a string) string {
	return strings.Trim(strings.TrimSpace(a), "<>")
}

// recipientAsLogged is a recipient in the form Postfix writes it in "to=<..>"
// and "orig_to=<..>", so a delivery line finds its row. Postfix 3.10.13 keeps
// the address in internal form - a quoted local part unquoted, a backslash
// dropped before the character it escapes, a blank in quotes made a space
// (src/smtpd/smtpd.c, extract_addr(), over src/global/tok822_parse.c,
// COLLECT and tok822_internalize()) - and logs it in external form
// (src/global/info_log_addr_form.c with info_log_address_format=external,
// quote_822_local(): the local part quoted again only when it is not a plain
// dot-string). So "ab"@x is logged ab@x, and "a b"@x stays "a b"@x. Case is
// kept. An address with a comment or a blank outside quotes is taken as
// written: tok822 would drop or join them, and no application writes one.
func recipientAsLogged(a string) string {
	a = cleanAddress(a)
	at := strings.LastIndexByte(a, '@')
	local, domain := a, ""
	if at >= 0 {
		local, domain = a[:at], a[at:]
	}
	if len(local) >= 2 && local[0] == '"' && local[len(local)-1] == '"' {
		var b strings.Builder
		for i := 1; i < len(local)-1; i++ {
			c := local[i]
			if c == '\\' && i+1 < len(local)-1 {
				i++
				c = local[i]
			}
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				c = ' '
			}
			b.WriteByte(c)
		}
		local = b.String()
	}
	return quoteLocalPart(local) + domain
}

// quoteLocalPart is quote_822_local() of Postfix 3.10.13
// (src/global/quote_822_local.c, with QUOTE_FLAG_8BITCLEAN, the default): a
// local part that is a dot-string - atoms of no blank, control character or
// one of ()<>@,;:\"[], joined by single dots, none first or last - is left as
// it is; any other is put in quotes, with a backslash before ", \ and CR.
func quoteLocalPart(local string) string {
	if isDotString(local) {
		return local
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(local); i++ {
		c := local[i]
		if c == '"' || c == '\\' || c == '\r' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// isDotString is is_822_dot_string() of quote_822_local.c, 8-bit clean.
func isDotString(s string) bool {
	if s == "" || s[0] == '.' || s[len(s)-1] == '.' || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c < 32 || c == 127 || strings.IndexByte(`()<>@,;:\"[]`, c) >= 0 {
			return false
		}
	}
	return true
}

// domainOf returns the lower-cased domain of an email address, or "" if there
// is no domain part. Sender binding guarantees the From domain equals the
// application's domain, so this is the sending domain (architecture.md § Mail
// path).
func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

// Serve runs the journal-milter on ln until ctx is cancelled. Each connection
// gets a fresh session bound to rec. It returns nil on a clean shutdown.
//
// go-milter v0.4.1, server.go: Close() closes the listener and Serve() then
// returns ErrServerClosed. Postfix 3.10.13, src/milter/milter8.c,
// milter8_message(): SMFIP_NOBODY leaves out only the body chunks - headers,
// end-of-headers and end-of-body (milter8_eob()) still go; milter8_event()
// takes a reply code (SMFIR_REPLYCODE) whatever the action mask.
func Serve(ctx context.Context, ln net.Listener, rec Store) error {
	flight := &inflight{} // shared: the level-2 window spans all connections
	srv := &milter.Server{
		NewMilter: func() milter.Milter { return &session{rec: rec, flight: flight} },
		Actions:   0,                // read-only: we make no message modifications
		Protocol:  milter.OptNoBody, // the journal needs headers/EOM, not the body
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	if err := srv.Serve(ln); err != nil {
		if ctx.Err() != nil {
			return nil // expected: Close() during shutdown unblocks Serve
		}
		return err
	}
	return nil
}
