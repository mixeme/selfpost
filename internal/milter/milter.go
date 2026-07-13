// Package milter implements the SelfPost journal-milter: a lightweight milter
// (spec 7.3) attached to Postfix's smtpd_milters alongside OpenDKIM. On the
// receive path it reads the SASL login, From, recipients and Subject of each
// accepted message and records one send-log row per (queue-id, recipient),
// giving the panel a structured, filterable history that raw mail.log cannot.
//
// It is monitoring only: it never rejects, and every callback returns Continue
// or Accept so a failure of this milter can never block the relay. Postfix is
// configured with default_action=accept for this milter's socket, so even a
// crash or hang fails open (spec 7.3).
package milter

import (
	"context"
	"log"
	"net"
	"net/textproto"
	"strings"

	"github.com/emersion/go-milter"

	"codeberg.org/mix/selfpost/internal/store"
)

// Recorder persists queued send-log entries. *store.Store satisfies it; tests
// substitute a fake.
type Recorder interface {
	InsertQueued(e store.SendLogEntry) error
}

// session accumulates the fields of one message as the milter callbacks fire.
// Milter macros arrive per-stage and do not accumulate, so each value is
// captured at the stage that carries it (spec 7.3 / Phase 0 spike): SASL login
// and From at MAIL, each recipient at RCPT, Subject in the headers, and the
// queue-id at end-of-message. go-milter creates one session per connection; a
// connection may carry several messages, so per-message fields are reset at
// MailFrom (the start of every transaction).
type session struct {
	milter.NoOpMilter
	rec Recorder

	clientIP string // captured once per connection

	login   string
	from    string
	rcpts   []string
	subject string
}

// Connect captures the client IP, which comes from the addr parameter rather
// than a macro (the {client_addr} macro was empty in the spike). It is the
// rate-limit key for Phase 8; here it is recorded for completeness.
func (s *session) Connect(host, family string, port uint16, addr net.IP, m *milter.Modifier) (milter.Response, error) {
	if addr != nil {
		s.clientIP = addr.String()
	}
	return milter.RespContinue, nil
}

// MailFrom starts a new message: reset per-message state, then capture the
// envelope sender and the SASL login ({auth_authen}, carried by the MAIL-stage
// macros).
func (s *session) MailFrom(from string, m *milter.Modifier) (milter.Response, error) {
	s.from = cleanAddress(from)
	s.login = macro(m, "auth_authen")
	s.rcpts = nil
	s.subject = ""
	return milter.RespContinue, nil
}

// RcptTo records each recipient. Postfix calls this once per recipient, which
// is what lets the journal keep a separate row per (queue-id, recipient).
func (s *session) RcptTo(rcpt string, m *milter.Modifier) (milter.Response, error) {
	s.rcpts = append(s.rcpts, cleanAddress(rcpt))
	return milter.RespContinue, nil
}

// Header captures the Subject. Only the first Subject header is kept.
func (s *session) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	if s.subject == "" && textproto.CanonicalMIMEHeaderKey(name) == "Subject" {
		s.subject = value
	}
	return milter.RespContinue, nil
}

// Body fires at end-of-message, when the queue-id macro {i} is set and the
// message is about to be committed to the queue. This is where the "queued"
// rows are written. We accept (this milter is done) without ever rejecting.
func (s *session) Body(m *milter.Modifier) (milter.Response, error) {
	s.record(macro(m, "i"))
	return milter.RespAccept, nil
}

// macro reads a milter macro, tolerating Postfix's convention of wrapping
// multi-character macro names in curly braces (e.g. {auth_authen}) while
// single-character names (e.g. i) arrive bare. go-milter stores whatever name
// Postfix sends verbatim, so a lookup must try both forms — this is exactly the
// distinction the SASL-less Phase 0 spike could not observe.
func macro(m *milter.Modifier, name string) string {
	if v, ok := m.Macros[name]; ok {
		return v
	}
	return m.Macros["{"+name+"}"]
}

// record writes one send-log row per recipient. Failures are logged, never
// propagated: journalling must not affect mail acceptance (spec 7.3).
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

// cleanAddress strips the angle brackets and any ESMTP parameters Postfix may
// pass with an address, leaving the bare mailbox.
func cleanAddress(a string) string {
	a = strings.TrimSpace(a)
	if i := strings.IndexByte(a, ' '); i >= 0 { // drop "addr SIZE=… BODY=…" params
		a = a[:i]
	}
	a = strings.TrimPrefix(a, "<")
	a = strings.TrimSuffix(a, ">")
	return a
}

// domainOf returns the lower-cased domain of an email address, or "" if there
// is no domain part. Sender binding (Phase 4) guarantees the From domain equals
// the application's domain, so this is the sending domain (spec 7.3).
func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

// Serve runs the journal-milter on ln until ctx is cancelled. Each connection
// gets a fresh session bound to rec. It returns nil on a clean shutdown.
func Serve(ctx context.Context, ln net.Listener, rec Recorder) error {
	srv := &milter.Server{
		NewMilter: func() milter.Milter { return &session{rec: rec} },
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
