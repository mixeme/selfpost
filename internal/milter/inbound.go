package milter

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/emersion/go-milter"

	"github.com/mixeme/selfpost/internal/mailhdr"
	"github.com/mixeme/selfpost/internal/store"
)

// InboundStore is what the inbound journal milter needs: the journal itself
// and the list of inbound domains, since port 25 also takes mail that is not
// the relay's. *store.Store satisfies it; tests substitute a fake.
type InboundStore interface {
	InsertInboundSpam(e store.InboundSpamEntry) error
	IsInboundDomain(name string) (bool, error)
	ForgetInboundQueueID(queueID string) error
}

// inboundSession is the inbound journal milter: the last milter on port 25,
// after the optional external filter. It writes one journal row per recipient
// of a message that reached end-of-message - that is, one the filter did not
// refuse; refusals never get this far and are read from mail.log instead.
//
// Like the send-log milter it only watches: every callback continues, and
// Postfix attaches the socket with default_action=accept, so
// neither an error here nor the panel being down holds up inbound mail.
//
// Postfix 3.10.13, src/milter/milter.c, milter_message(): the milters are asked
// in turn while the reply is null, so one that refuses is the last asked;
// src/milter/milter8.c, milter8_comm_error(): default_action=accept gives no
// reply when this milter cannot answer.
type inboundSession struct {
	milter.NoOpMilter
	rec InboundStore
	// key is what the filter signs each message with (FilterSignature), or
	// empty when no filter is attached. A verdict is read only next to a
	// signature that fits: the filter is attached so that mail flows when it is
	// down, and then the verdict headers of a message are whatever its sender
	// wrote.
	key string

	// clientIP is taken at Connect and again from {client_addr} with every
	// MAIL FROM: a session the library made after a non-continue reply never
	// sees Connect (see session.MailFrom in milter.go).
	clientIP string

	from    string
	rcpts   []string
	subject string
	action  string // X-Rspamd-Action
	result  string // X-Spamd-Result
	seen    bool   // any verdict header of the filter
	sig     string // X-SelfPost-Filter
}

func (s *inboundSession) Connect(host, family string, port uint16, addr net.IP, m *milter.Modifier) (milter.Response, error) {
	if addr != nil {
		s.clientIP = addr.String()
	}
	return milter.RespContinue, nil
}

// MailFrom starts a message: a connection may carry several.
func (s *inboundSession) MailFrom(from string, m *milter.Modifier) (milter.Response, error) {
	s.reset()
	if ip := clientAddr(m); ip != "" {
		s.clientIP = ip
	}
	s.from = cleanAddress(from)
	return milter.RespContinue, nil
}

// RcptTo keeps the recipient as Postfix logs it, so that the delivery lines
// that carry the upstream's answer find the row (recipientAsLogged): a quoted
// local part that needs no quotes is written without them.
func (s *inboundSession) RcptTo(rcpt string, m *milter.Modifier) (milter.Response, error) {
	s.rcpts = append(s.rcpts, recipientAsLogged(rcpt))
	return milter.RespContinue, nil
}

// Header keeps the first Subject, the filter's verdict and its signature.
// Postfix hands the message to its milters one after another at
// end-of-message, each seeing what the one before added, which is how the
// filter's headers arrive here. The filter replaces headers of these names, so
// the last one read is its own - when it is up. Whether it was is decided at
// end-of-message, by the signature.
//
// Postfix 3.10.13, src/milter/milter.c, milter_message(), hands each milter the
// queue file from the start of the content; src/milter/milter8.c,
// milter8_message(), reads it with rec_get(), which follows the pointer
// records src/cleanup/cleanup_milter.c, cleanup_add_header(), leaves after
// the last header for a header the milter before added.
// rspamd 3.11.0, src/plugins/lua/milter_headers.lua, routines x-spamd-result
// and x-rspamd-action: remove = 0, every instance
// (src/libserver/milter.c, rspamd_milter_remove_header_safe()), and the new
// one added at the end (order -1, ADDHEADER) - when skip_wanted() does not
// skip the routine (skip_local, skip_authenticated).
func (s *inboundSession) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	switch textproto.CanonicalMIMEHeaderKey(name) {
	case "Subject":
		if s.subject == "" {
			s.subject = mailhdr.DecodeSubject(value)
		}
	case "X-Rspamd-Action":
		s.action, s.seen = value, true
	case "X-Spamd-Result":
		s.result, s.seen = value, true
	case signatureHeader:
		s.sig = strings.TrimSpace(value)
	}
	return milter.RespContinue, nil
}

// SignatureHeader is the header the filter signs a message in
// (deploy/antispam/rspamd/milter_headers.conf writes it).
const SignatureHeader = "X-SelfPost-Filter"

var signatureHeader = textproto.CanonicalMIMEHeaderKey(SignatureHeader)

// FilterSignature is what the filter writes into SignatureHeader for a message:
// the HMAC-SHA256 of its queue id under the key SelfPost shares with the
// filter, in hex. The key is used as the text it is stored as. A signature
// fits one queue id, so the header going on to the upstream with the mail
// gives nothing to whoever reads it there.
//
// Measured, not read: what rspamd 3.11 writes for a key and a queue id was
// taken in a throwaway container on 2026-10-12
// (TestFilterSignatureIsWhatTheFilterWrites).
func FilterSignature(key, queueID string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(queueID))
	return hex.EncodeToString(mac.Sum(nil))
}

// signed reports whether the message carries the filter's signature for its
// queue id. No key, no queue id or no signature is "no".
func (s *inboundSession) signed(queueID string) bool {
	if s.key == "" || queueID == "" || s.sig == "" {
		return false
	}
	return hmac.Equal([]byte(strings.ToLower(s.sig)), []byte(FilterSignature(s.key, queueID)))
}

// Body fires at end-of-message: the message is about to be queued for its
// upstream. "Continue" lets it through as "accept" would, and keeps this
// session for the connection's next message.
//
// The queue id: Postfix 3.10.13, src/global/mail_params.h, DEF_MILT_EOD_MACROS
// "i", answered by src/cleanup/cleanup_milter.c, cleanup_milter_eval(). The
// rest as session.Body in milter.go.
func (s *inboundSession) Body(m *milter.Modifier) (milter.Response, error) {
	// The queue id is known only now, and with it whether the verdict headers
	// read on the way here are the filter's. The rows keep it: the delivery
	// lines that put the upstream's answer on them name it (logtail).
	queueID := macro(m, "i")
	if !s.signed(queueID) {
		s.seen = false
	}
	s.record(queueID)
	s.reset()
	return milter.RespContinue, nil
}

func (s *inboundSession) Abort(m *milter.Modifier) error {
	s.reset()
	return nil
}

func (s *inboundSession) reset() {
	s.from, s.rcpts, s.subject = "", nil, ""
	s.action, s.result, s.seen, s.sig = "", "", false, ""
}

// record writes one row per recipient at an inbound domain, with the queue id
// of the message and "queued" - "held" when the filter's verdict had Postfix
// hold it (src/cleanup/cleanup_milter.c: a milter's HOLD puts the message in
// the hold queue, which qmgr does not schedule). The message is about to be
// queued for its upstream, and the rows are written before Postfix goes on,
// so before any delivery line of it; older rows of a queue id Postfix uses
// again are detached first. With no queue id (none came) the rows have no
// result: nothing could report one. Other recipients of port 25 (a DMARC
// report address at the server's own name) are not the relay's mail and get
// no row. Failures are logged, never propagated.
func (s *inboundSession) record(queueID string) {
	e := store.InboundSpamEntry{
		ClientIP: s.clientIP,
		From:     s.from,
		Subject:  s.subject,
		Decision: store.SpamDecisionAccept,
		Engine:   store.SpamEngineSelfPost,
	}
	if s.seen {
		action, detail := rspamdVerdict(s.action, s.result)
		e.Engine, e.Detail = store.SpamEngineRspamd, detail
		if action == "quarantine" {
			e.Decision = store.SpamDecisionQuarantine
		}
	}
	if queueID != "" {
		e.QueueID, e.Upstream = queueID, store.UpstreamQueued
		if e.Decision == store.SpamDecisionQuarantine {
			e.Upstream = store.UpstreamHeld
		}
		if err := s.rec.ForgetInboundQueueID(queueID); err != nil {
			log.Printf("inbound-journal: detach older rows of %s: %v", queueID, err)
		}
	}
	for _, to := range s.rcpts {
		domain := domainOf(to)
		ok, err := s.rec.IsInboundDomain(domain)
		if err != nil {
			log.Printf("inbound-journal: look up %s: %v", domain, err)
			continue
		}
		if !ok {
			continue
		}
		e.InboundDomain, e.To = domain, to
		if err := s.rec.InsertInboundSpam(e); err != nil {
			log.Printf("inbound-journal: record %s -> %s: %v", s.from, to, err)
		}
	}
}

// What the journal keeps of a verdict: enough to see why, on one line.
const (
	verdictMaxSymbols = 6
	verdictMaxBytes   = 255
)

// ListSymbolPrefix starts the names of the symbols the filter lists' rules add
// (deploy/antispam/rspamd/multimap.conf): SELFPOST_ALLOW_IP, _FROM, _DOMAIN.
const ListSymbolPrefix = "SELFPOST_"

// rspamd 3.11.0, src/plugins/lua/milter_headers.lua, routine x-spamd-result:
// string.format('default: %s [%.2f / %.2f]', ...), then one
// '%s(%.2f)[%s]' per symbol, joined by '; '.
var (
	// "default: False [7.20 / 15.00]" - the score and the reject threshold.
	rspamdScore = regexp.MustCompile(`\[(-?[0-9]+(?:\.[0-9]+)?) / (-?[0-9]+(?:\.[0-9]+)?)\]`)
	// "BAYES_SPAM(5.10)[100.00%]" - a symbol and its weight.
	rspamdSymbol = regexp.MustCompile(`([A-Z][A-Z0-9_]{1,63})\((-?[0-9]+(?:\.[0-9]+)?)\)`)
)

// rspamdVerdict reduces the filter's headers to an action and a one-line
// summary: the action, the score against the threshold, and the first few
// symbols that carried weight. It reads only what it recognises - the headers
// are read only next to the filter's signature, and even so nothing of them is
// kept verbatim except through these patterns.
func rspamdVerdict(actionHeader, resultHeader string) (action, detail string) {
	action = strings.ToLower(strings.TrimSpace(actionHeader))
	for _, r := range action {
		if (r < 'a' || r > 'z') && r != ' ' {
			action = ""
			break
		}
	}
	if len(action) > 32 {
		action = ""
	}

	var parts []string
	if action != "" {
		parts = append(parts, action)
	}
	if m := rspamdScore.FindStringSubmatch(resultHeader); m != nil {
		parts = append(parts, "score "+m[1]+" / "+m[2])
	}
	detail = strings.Join(parts, ", ")

	// The filter lists' own symbols come first and are kept whatever else there
	// is: Filter lists counts how often an entry decided by finding them here,
	// and rspamd writes symbols heaviest first, which puts an allow entry's
	// -100 last, behind any six others.
	var symbols, rest []string
	for _, m := range rspamdSymbol.FindAllStringSubmatch(resultHeader, -1) {
		if strings.Trim(m[2], "-0.") == "" { // weight zero: informational
			continue
		}
		if strings.HasPrefix(m[1], ListSymbolPrefix) {
			if len(symbols) < verdictMaxSymbols {
				symbols = append(symbols, m[1]+"("+m[2]+")")
			}
			continue
		}
		rest = append(rest, m[1]+"("+m[2]+")")
	}
	for _, sym := range rest {
		if len(symbols) == verdictMaxSymbols {
			break
		}
		symbols = append(symbols, sym)
	}
	if len(symbols) > 0 {
		if detail != "" {
			detail += "; "
		}
		detail += strings.Join(symbols, " ")
	}
	if len(detail) > verdictMaxBytes {
		detail = detail[:verdictMaxBytes]
	}
	return action, detail
}

// ServeInbound runs the inbound journal milter on ln until ctx is cancelled.
// key is what the external filter in front of it signs messages with, or
// empty when none is attached (INBOUND_ANTISPAM_MILTER): no verdict is then
// read at all. It returns nil on a clean shutdown. What it relies on of
// go-milter and Postfix is cited at Serve in milter.go.
func ServeInbound(ctx context.Context, ln net.Listener, rec InboundStore, key string) error {
	srv := &milter.Server{
		NewMilter: func() milter.Milter { return &inboundSession{rec: rec, key: key} },
		Actions:   0,                // read-only
		Protocol:  milter.OptNoBody, // headers and end-of-message, not the body
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
