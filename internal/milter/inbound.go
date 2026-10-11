package milter

import (
	"context"
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
}

// inboundSession is the inbound journal milter: the last milter on port 25,
// after the optional external filter. It writes one journal row per recipient
// of a message that reached end-of-message - that is, one the filter did not
// refuse; refusals never get this far and are read from mail.log instead.
//
// Like the send-log milter it only watches: every callback continues or
// accepts, and Postfix attaches the socket with default_action=accept, so
// neither an error here nor the panel being down holds up inbound mail.
type inboundSession struct {
	milter.NoOpMilter
	rec InboundStore
	// filter says whether an external filter is attached in front of this
	// milter. Without one the verdict headers below can only have been written
	// by the sender, and are not read.
	filter bool

	clientIP string // once per connection

	from    string
	rcpts   []string
	subject string
	action  string // X-Rspamd-Action
	result  string // X-Spamd-Result
	seen    bool   // any verdict header of the filter
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
	s.from = cleanAddress(from)
	return milter.RespContinue, nil
}

func (s *inboundSession) RcptTo(rcpt string, m *milter.Modifier) (milter.Response, error) {
	s.rcpts = append(s.rcpts, cleanAddress(rcpt))
	return milter.RespContinue, nil
}

// Header keeps the first Subject and the filter's verdict. Postfix hands the
// message to its milters one after another at end-of-message, each seeing what
// the one before added, which is how the filter's headers arrive here. The
// filter replaces headers of these names, so the last one read is its own.
func (s *inboundSession) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	switch textproto.CanonicalMIMEHeaderKey(name) {
	case "Subject":
		if s.subject == "" {
			s.subject = mailhdr.DecodeSubject(value)
		}
	case "X-Rspamd-Action":
		if s.filter {
			s.action, s.seen = value, true
		}
	case "X-Spamd-Result":
		if s.filter {
			s.result, s.seen = value, true
		}
	}
	return milter.RespContinue, nil
}

// Body fires at end-of-message: the message is about to be queued for its
// upstream.
func (s *inboundSession) Body(m *milter.Modifier) (milter.Response, error) {
	s.record()
	s.reset()
	return milter.RespAccept, nil
}

func (s *inboundSession) Abort(m *milter.Modifier) error {
	s.reset()
	return nil
}

func (s *inboundSession) reset() {
	s.from, s.rcpts, s.subject = "", nil, ""
	s.action, s.result, s.seen = "", "", false
}

// record writes one row per recipient at an inbound domain. Other recipients
// of port 25 (a DMARC report address at the server's own name) are not the
// relay's mail and get no row. Failures are logged, never propagated.
func (s *inboundSession) record() {
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

var (
	// "default: False [7.20 / 15.00]" - the score and the reject threshold.
	rspamdScore = regexp.MustCompile(`\[(-?[0-9]+(?:\.[0-9]+)?) / (-?[0-9]+(?:\.[0-9]+)?)\]`)
	// "BAYES_SPAM(5.10)[100.00%]" - a symbol and its weight.
	rspamdSymbol = regexp.MustCompile(`([A-Z][A-Z0-9_]{1,63})\((-?[0-9]+(?:\.[0-9]+)?)\)`)
)

// rspamdVerdict reduces the filter's headers to an action and a one-line
// summary: the action, the score against the threshold, and the first few
// symbols that carried weight. It reads only what it recognises - the headers
// are the filter's when it is up, but the sender's own when it is not (it is
// attached fail-open), so nothing of them is kept verbatim except through
// these patterns.
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
// filter says whether an external filter is attached in front of it
// (INBOUND_ANTISPAM_MILTER). It returns nil on a clean shutdown.
func ServeInbound(ctx context.Context, ln net.Listener, rec InboundStore, filter bool) error {
	srv := &milter.Server{
		NewMilter: func() milter.Milter { return &inboundSession{rec: rec, filter: filter} },
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
