// Package notify writes to the panel's users when something needs them
// (architecture.md § Notifications): a sending domain's DNS record does not
// match, a level-2 rate limit refuses mail, the TLS certificate is about to
// expire, a message has waited in the queue for more than three days.
//
// Each subject has a row of state in the database, so a thing is said once and
// a restart says nothing again. The rules of when a message goes out are the
// owner's (docs/architecture.md § Notifications) and
// live in this file; what the messages say is in text.go, how they leave in
// mailer.go.
package notify

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

const (
	// CheckInterval is how often DNS, the certificate and the queue are looked
	// at.
	CheckInterval = time.Hour
	// LimitInterval is how often refused mail is looked for. A refusal is
	// reported within the minute rather than on the hour: it is mail not being
	// sent now. The look reads the refused rows through an index of their own
	// (migration 0005).
	LimitInterval = time.Minute

	// dnsChecks is how many checks in a row must agree before a DNS mismatch is
	// reported, and before it is reported gone; dnsApart is how far apart the
	// first and the last of them must be. One check is a resolver's bad minute;
	// two, an hour apart, is a record that is wrong. The time is asked for as
	// well as the count because the first check after a start is minutes after
	// the last one before it.
	dnsChecks = 2
	dnsApart  = 45 * time.Minute

	// QueueAge is how long a message waits before the queue is reported. Postfix
	// keeps a message for five days by default, so two are left to act in.
	QueueAge = 3 * 24 * time.Hour

	// The certificate is reported this many days before it expires, and again at
	// the second number if it is still the same certificate.
	certFirstDays = 14
	certLastDays  = 3

	// limitRepeat is the shortest time between two messages about the limits of
	// the same domain.
	limitRepeat = 24 * time.Hour

	// hourlyCeiling is the most messages handed to Postfix in an hour, whatever
	// the rules above arrive at. They bypass every rate limit, and the number of
	// subjects grows with the number of domains; nothing here should ever come
	// near it, and a fault that does is stopped by it.
	hourlyCeiling = 60

	phasePending = "pending" // seen wrong, not yet said
	phaseOpen    = "open"    // said
)

// Store is what the notifier needs of the database. *store.Store satisfies it.
type Store interface {
	NotificationsEnabled() (bool, error)
	NotifyRecipients(domainName string) ([]string, error)
	GetNotifyState(key string) (store.NotifyState, bool, error)
	SetNotifyState(st store.NotifyState) error
	DeleteNotifyState(key string) error
	ListNotifyStateKeys(prefix string) ([]string, error)
	NotifyAddressees(key string) (map[string]bool, error)
	SetNotifyAddressees(key string, owed map[string]bool, at time.Time) error
	LimitRefusalsSince(since time.Time) ([]store.LimitRefusal, error)
	ListDomains() ([]store.Domain, error)
}

// Sender hands one message to the mail system. *Mailer does; tests collect.
type Sender interface {
	Send(to, subject, body string) error
}

// Checks are the readings the notifier acts on, as functions so that tests
// need neither DNS nor a certificate nor a queue.
type Checks struct {
	// Domain is a fresh check of a sending domain's published records.
	Domain func(d store.Domain) (dnscheck.Domain, error)
	// Certificate is the state of the TLS certificate Postfix serves.
	Certificate func() health.Certificate
	// Queue lists the messages Postfix holds; an error says nothing about any.
	Queue func() ([]postfix.QueuedMessage, error)
}

// Notifier applies the rules. Create one with New and call Run.
type Notifier struct {
	st       Store
	checks   Checks
	sender   Sender
	hostname string
	now      func() time.Time
	// ctx ends a pass between two domains when the process is asked to stop.
	ctx context.Context
	// sentAt holds the times of the messages of the last hour, for the ceiling.
	sentAt []time.Time
}

// New builds a notifier for the server named hostname.
func New(st Store, checks Checks, sender Sender, hostname string) *Notifier {
	return &Notifier{
		st: st, checks: checks, sender: sender, hostname: hostname,
		now: func() time.Time { return time.Now().UTC() },
		ctx: context.Background(),
	}
}

// Run checks on both cadences until ctx is cancelled. The first hourly check
// runs a few minutes after the start, not at it: a container that has just
// come up is still starting Postfix and may have no network yet.
func (n *Notifier) Run(ctx context.Context) error {
	n.ctx = ctx
	first := time.NewTimer(3 * time.Minute)
	defer first.Stop()
	hourly := time.NewTicker(CheckInterval)
	defer hourly.Stop()
	limits := time.NewTicker(LimitInterval)
	defer limits.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-first.C:
			n.Check()
		case <-hourly.C:
			n.Check()
		case <-limits.C:
			n.CheckLimits()
		}
	}
}

// enabled reports whether anything is to be done at all. With the instance
// switch off nothing is checked and no state moves, so that what is wrong
// when it is turned on again is said then.
func (n *Notifier) enabled() bool {
	on, err := n.st.NotificationsEnabled()
	if err != nil {
		log.Printf("notify: read the switch: %v", err)
		return false
	}
	return on
}

// Check is the hourly pass: DNS of every sending domain, the certificate, the
// queue.
func (n *Notifier) Check() {
	if !n.enabled() {
		return
	}
	n.checkDNS()
	n.checkCertificate()
	n.checkQueue()
}

// outcome is what became of a message.
type outcome int

const (
	// sent: at least one recipient's copy was handed to Postfix.
	sent outcome = iota
	// nobody: no user is to be told - none with an e-mail and the switch on.
	nobody
	// failed: there were recipients and none could be handed a copy.
	failed
)

// A message is owed to each recipient, not to the subject (owner,
// 2026-10-14). The first message of a subject goes to everyone who is to hear
// of it then (open), and who was handed a copy is recorded; while the subject
// is open, each later check hands it to those owed it who have not had it -
// a copy that failed, or that the hourly ceiling held back - and who are
// still to hear of it (followUp); a user who comes to hear of the subject
// only while it is open is not mailed about it. That it is over goes to
// those who were told (closed).

// deliver hands m to each address, each in a message of its own - users are
// not shown each other's addresses - under the hourly ceiling, and returns
// those that were handed a copy.
func (n *Notifier) deliver(to []string, m message) []string {
	var handed []string
	for _, addr := range to {
		if !n.underCeiling() {
			log.Printf("notify: more than %d messages in an hour; %q to %s is held back", hourlyCeiling, m.subject, addr)
			continue
		}
		if err := n.sender.Send(addr, m.subject, m.body); err != nil {
			log.Printf("notify: send %q to %s: %v", m.subject, addr, err)
			continue
		}
		n.sentAt = append(n.sentAt, n.now())
		handed = append(handed, addr)
	}
	if len(to) > 0 {
		log.Printf("notify: %q sent to %d of %d", m.subject, len(handed), len(to))
	}
	return handed
}

// recipients are who is to hear of a subject of domainName now ("" for the
// server's own subjects).
func (n *Notifier) recipients(domainName string) ([]string, bool) {
	to, err := n.st.NotifyRecipients(domainName)
	if err != nil {
		log.Printf("notify: recipients: %v", err)
		return nil, false
	}
	return to, true
}

// open says m, the first message of the subject key, to everyone who is to
// hear of it, and records who was handed a copy.
//
// What the caller makes of the outcome is the rule that keeps a thing from
// being said never: a problem counts as said only when it was sent, so one
// that arose while nobody could be told, or while the mail system was down, is
// told at a later check.
func (n *Notifier) open(key, domainName string, m message) outcome {
	to, ok := n.recipients(domainName)
	if !ok {
		return failed
	}
	if len(to) == 0 {
		return nobody
	}
	handed := n.deliver(to, m)
	if len(handed) == 0 {
		return failed
	}
	owed := make(map[string]bool, len(to))
	for _, addr := range to {
		owed[addr] = false
	}
	for _, addr := range handed {
		owed[addr] = true
	}
	if err := n.st.SetNotifyAddressees(key, owed, n.now()); err != nil {
		log.Printf("notify: %v", err)
	}
	return sent
}

// followUp hands m, the message of an open subject, to those it is owed to
// who have not had it and are still to hear of it.
func (n *Notifier) followUp(key, domainName string, m message) {
	owed, err := n.st.NotifyAddressees(key)
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	var due []string
	for addr, told := range owed {
		if !told {
			due = append(due, addr)
		}
	}
	if len(due) == 0 {
		return
	}
	now, ok := n.recipients(domainName)
	if !ok {
		return
	}
	due = intersect(due, now)
	handed := n.deliver(due, m)
	if len(handed) == 0 {
		return
	}
	for _, addr := range handed {
		owed[addr] = true
	}
	if err := n.st.SetNotifyAddressees(key, owed, n.now()); err != nil {
		log.Printf("notify: %v", err)
	}
}

// closed says m - the subject is over - to those who were told of it and are
// still to hear of it. A subject said before 2.5.4 has no record of who was
// told, and its end goes to everyone who is to hear of it, as it did then.
func (n *Notifier) closed(key, domainName string, m message) outcome {
	owed, err := n.st.NotifyAddressees(key)
	if err != nil {
		log.Printf("notify: %v", err)
		return failed
	}
	to, ok := n.recipients(domainName)
	if !ok {
		return failed
	}
	if len(owed) > 0 {
		var told []string
		for addr, t := range owed {
			if t {
				told = append(told, addr)
			}
		}
		to = intersect(told, to)
	}
	if len(to) == 0 {
		return nobody
	}
	if len(n.deliver(to, m)) == 0 {
		return failed
	}
	return sent
}

// intersect is the addresses of a that are in b, in b's order.
func intersect(a, b []string) []string {
	in := make(map[string]bool, len(a))
	for _, x := range a {
		in[x] = true
	}
	var out []string
	for _, x := range b {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}

// underCeiling reports whether one more message may go out this hour.
func (n *Notifier) underCeiling() bool {
	cutoff := n.now().Add(-time.Hour)
	keep := n.sentAt[:0]
	for _, t := range n.sentAt {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	n.sentAt = keep
	return len(n.sentAt) < hourlyCeiling
}

func (n *Notifier) save(st store.NotifyState) {
	if err := n.st.SetNotifyState(st); err != nil {
		log.Printf("notify: save state %s: %v", st.Key, err)
	}
}

func (n *Notifier) forget(key string) {
	if err := n.st.DeleteNotifyState(key); err != nil {
		log.Printf("notify: forget state %s: %v", key, err)
	}
}

// ---- DNS

// dnsFault is what is wrong with a domain's records: the checks that failed,
// in the order the page shows them.
type dnsFault struct {
	Record string // "DKIM", "SPF", "DMARC"
	Detail string
}

// dnsReading sorts a check into wrong, right, or nothing learned. A record
// that fails is wrong whatever the others say. A lookup that did not answer is
// nothing learned - unless another record is plainly wrong. A warning (no DMARC
// record, an SPF include this check does not follow) is advice the page gives
// and not a thing to write about.
func dnsReading(d dnscheck.Domain) (faults []dnsFault, known bool) {
	known = true
	for _, r := range []struct {
		name string
		res  dnscheck.Result
	}{{"DKIM", d.DKIM}, {"SPF", d.SPF}, {"DMARC", d.DMARC}} {
		switch r.res.Status {
		case health.StatusError:
			faults = append(faults, dnsFault{Record: r.name, Detail: r.res.Detail})
		case health.StatusUnknown:
			known = false
		}
	}
	if len(faults) > 0 {
		known = true
	}
	return faults, known
}

func (n *Notifier) checkDNS() {
	domains, err := n.st.ListDomains()
	if err != nil {
		log.Printf("notify: list domains: %v", err)
		return
	}
	names := make(map[string]bool, len(domains))
	for _, d := range domains {
		names[d.Name] = true
	}
	for _, d := range domains {
		// A dead resolver costs seconds per domain; a process asked to stop
		// does not wait for the rest of them.
		if n.ctx.Err() != nil {
			return
		}
		res, err := n.checks.Domain(d)
		if err != nil {
			log.Printf("notify: check %s: %v", d.Name, err)
			continue
		}
		n.dnsStep(d.Name, res)
	}
	// A deleted domain has no records to be right or wrong.
	keys, err := n.st.ListNotifyStateKeys("dns:")
	if err != nil {
		log.Printf("notify: list dns state: %v", err)
		return
	}
	for _, key := range keys {
		if !names[strings.TrimPrefix(key, "dns:")] {
			n.forget(key)
		}
	}
}

// dnsStep moves one domain's state by one check.
//
// A subject is "pending" from the first bad check, with Since the time of it,
// and "open" from the message. While open, Count is the good checks in a row
// and Mark the time of the first of them.
func (n *Notifier) dnsStep(name string, res dnscheck.Domain) {
	faults, known := dnsReading(res)
	if !known {
		return
	}
	bad := len(faults) > 0
	key := "dns:" + name
	st, have, err := n.st.GetNotifyState(key)
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	now := n.now()

	switch {
	case !have && !bad:
		// Nothing wrong and nothing said.
	case !have && bad:
		n.save(store.NotifyState{Key: key, Phase: phasePending, Count: 1, Since: now})
	case st.Phase == phasePending && !bad:
		n.forget(key) // it was the resolver's bad minute
	case st.Phase == phasePending && bad:
		st.Count++
		if st.Count >= dnsChecks && now.Sub(st.Since) >= dnsApart {
			if n.open(key, name, dnsProblem(n.hostname, name, faults, st.Since)) == sent {
				st.Phase, st.Count, st.Mark, st.SentAt = phaseOpen, 0, "", now
			}
		}
		n.save(st)
	case st.Phase == phaseOpen && bad:
		if st.Count != 0 {
			// It went right for a check and wrong again: still the same trouble.
			st.Count, st.Mark = 0, ""
			n.save(st)
		}
		n.followUp(key, name, dnsProblem(n.hostname, name, faults, st.Since))
	case st.Phase == phaseOpen && !bad:
		st.Count++
		if st.Count == 1 {
			st.Mark = now.Format(time.RFC3339)
		}
		firstGood, _ := time.Parse(time.RFC3339, st.Mark)
		if st.Count >= dnsChecks && now.Sub(firstGood) >= dnsApart {
			// With nobody to tell any more, the trouble is over all the same.
			if n.closed(key, name, dnsCleared(n.hostname, name, st.Since)) != failed {
				n.forget(key)
				return
			}
		}
		n.save(st)
	}
}

// ---- certificate

func (n *Notifier) checkCertificate() {
	c := n.checks.Certificate()
	if c.NotAfter.IsZero() {
		return // no certificate configured, or one that cannot be read: Health says so
	}
	if c.Renewal != nil && !c.Renewal.Late {
		return // a renewal is mounted and Postfix takes it up at its next check
	}
	const key = "cert"
	mark := c.NotAfter.UTC().Format(time.RFC3339)
	st, have, err := n.st.GetNotifyState(key)
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	if have && st.Mark != mark {
		// A different certificate: what was said about the old one is over.
		n.forget(key)
		have, st = false, store.NotifyState{}
	}

	left := c.NotAfter.Sub(n.now())
	level := ""
	switch {
	case left <= certLastDays*24*time.Hour:
		level = "last"
	case left <= certFirstDays*24*time.Hour:
		level = "first"
	}
	if level == "" {
		return
	}
	if have && (st.Phase == level || st.Phase == "last") {
		n.followUp(key, "", certExpiring(n.hostname, c.NotAfter, left))
		return
	}
	if n.open(key, "", certExpiring(n.hostname, c.NotAfter, left)) != sent {
		return
	}
	n.save(store.NotifyState{Key: key, Phase: level, Mark: mark, Since: n.now(), SentAt: n.now()})
}

// ---- queue

// ownMail reports whether a queued message is the notifier's: a notification
// that could not be delivered yet, or the bounce of one coming back to
// postmaster@ this server, which receives no mail. Neither is the mail the
// queue is watched for, and reporting them would have one dead mailbox
// announce itself every three days.
func (n *Notifier) ownMail(m postfix.QueuedMessage) bool {
	own := From(n.hostname)
	if strings.EqualFold(m.Sender, own) {
		return true
	}
	for _, rcpt := range m.Recipients {
		if strings.EqualFold(rcpt, own) {
			return true
		}
	}
	return false
}

func (n *Notifier) checkQueue() {
	msgs, err := n.checks.Queue()
	if err != nil {
		log.Printf("notify: read the queue: %v", err)
		return // no listing is no information
	}
	now := n.now()
	var old []postfix.QueuedMessage
	for _, m := range msgs {
		if now.Sub(m.Arrival) > QueueAge && !n.ownMail(m) {
			old = append(old, m)
		}
	}
	sort.Slice(old, func(i, j int) bool { return old[i].Arrival.Before(old[j].Arrival) })

	const key = "queue"
	st, have, err := n.st.GetNotifyState(key)
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	switch {
	case len(old) > 0 && !have:
		if n.open(key, "", queueStuck(n.hostname, old, now)) == sent {
			n.save(store.NotifyState{Key: key, Phase: phaseOpen, Since: now, SentAt: now})
		}
	case len(old) > 0 && have:
		n.followUp(key, "", queueStuck(n.hostname, old, now))
	case len(old) == 0 && have:
		if n.closed(key, "", queueCleared(n.hostname, st.Since)) != failed {
			n.forget(key)
		}
	}
}

// ---- level-2 limits

// CheckLimits looks for mail refused under a level-2 rate limit and reports
// each sending domain whose applications were refused, at most once in
// limitRepeat.
func (n *Notifier) CheckLimits() {
	if !n.enabled() {
		return
	}
	now := n.now()
	refusals, err := n.st.LimitRefusalsSince(now.Add(-limitRepeat))
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	for _, r := range refusals {
		key := "limit:" + r.Domain
		st, have, err := n.st.GetNotifyState(key)
		if err != nil {
			log.Printf("notify: %v", err)
			continue
		}
		// Told within the day: the refusals counted now are the ones it was
		// told about, or more of the same. Told longer ago: everything in the
		// window is new since then.
		if have && now.Sub(st.SentAt) < limitRepeat {
			n.followUp(key, r.Domain, limitRefused(n.hostname, r))
			continue
		}
		if n.open(key, r.Domain, limitRefused(n.hostname, r)) == sent {
			n.save(store.NotifyState{Key: key, Phase: phaseOpen, Since: r.First, SentAt: now})
		}
	}

	// A domain nobody was refused under for two days is not a subject any more.
	keys, err := n.st.ListNotifyStateKeys("limit:")
	if err != nil {
		log.Printf("notify: list limit state: %v", err)
		return
	}
	for _, key := range keys {
		st, have, err := n.st.GetNotifyState(key)
		if err == nil && have && now.Sub(st.SentAt) > 2*limitRepeat {
			n.forget(key)
		}
	}
}
