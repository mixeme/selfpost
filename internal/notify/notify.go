// Package notify writes to the panel's users when something needs them
// (architecture.md § Notifications): a sending domain's DNS record stops
// matching, a level-2 rate limit refuses mail, the TLS certificate is about to
// expire, a message has waited in the queue for more than three days.
//
// Each subject has a row of state in the database, so a thing is said once and
// a restart says nothing again. The rules of when a message goes out are the
// owner's (docs/plans/panel-notifications.md § When a message is sent) and
// live in this file; what the messages say is in text.go, how they leave in
// mailer.go.
package notify

import (
	"context"
	"fmt"
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
	// at. A var-free constant: the rules below count in checks.
	CheckInterval = time.Hour
	// LimitInterval is how often refused mail is looked for. A refusal is
	// reported at once rather than on the hour: it is mail not being sent now.
	LimitInterval = 5 * time.Minute

	// dnsChecks is how many checks in a row must agree before a DNS mismatch is
	// reported, and before it is reported gone. One check is a resolver's bad
	// minute; two, an hour apart, is a record that is wrong.
	dnsChecks = 2

	// QueueAge is how long a message waits before the queue is reported. Postfix
	// keeps a message for five days by default, so two are left to act in.
	QueueAge = 3 * 24 * time.Hour

	// The certificate is reported this many days before it expires, and again at
	// the second number if it is still the same certificate.
	certFirstDays = 14
	certLastDays  = 3

	// limitRepeat is the shortest time between two messages about the same
	// level-2 limit.
	limitRepeat = 24 * time.Hour

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
}

// New builds a notifier for the server named hostname.
func New(st Store, checks Checks, sender Sender, hostname string) *Notifier {
	return &Notifier{st: st, checks: checks, sender: sender, hostname: hostname, now: func() time.Time { return time.Now().UTC() }}
}

// Run checks on both cadences until ctx is cancelled. The first hourly check
// runs a few minutes after the start, not at it: a container that has just
// come up is still starting Postfix and may have no network yet.
func (n *Notifier) Run(ctx context.Context) error {
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

// send writes one message to every recipient of a subject, each in a message
// of their own - users are not shown each other's addresses. It reports
// whether the subject counts as said: nobody to tell is said, a message that
// could not be handed over to anyone is not, and is tried at the next check.
func (n *Notifier) send(domainName string, m message) bool {
	to, err := n.st.NotifyRecipients(domainName)
	if err != nil {
		log.Printf("notify: recipients: %v", err)
		return false
	}
	if len(to) == 0 {
		return true
	}
	sent := 0
	for _, addr := range to {
		if err := n.sender.Send(addr, m.subject, m.body); err != nil {
			log.Printf("notify: send %q to %s: %v", m.subject, addr, err)
			continue
		}
		sent++
	}
	if sent > 0 {
		log.Printf("notify: %q sent to %d of %d", m.subject, sent, len(to))
	}
	return sent > 0
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
		if st.Count >= dnsChecks {
			if !n.send(name, dnsProblem(n.hostname, name, faults, st.Since)) {
				n.save(st)
				return
			}
			st.Phase, st.Count, st.SentAt = phaseOpen, 0, now
		}
		n.save(st)
	case st.Phase == phaseOpen && bad:
		if st.Count != 0 {
			st.Count = 0 // it went right for a check and wrong again: still the same trouble
			n.save(st)
		}
	case st.Phase == phaseOpen && !bad:
		st.Count++
		if st.Count >= dnsChecks {
			if n.send(name, dnsCleared(n.hostname, name, st.Since)) {
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
	if level == "" || (have && (st.Phase == level || st.Phase == "last")) {
		return
	}
	if !n.send("", certExpiring(n.hostname, c.NotAfter, left)) {
		return
	}
	n.save(store.NotifyState{Key: key, Phase: level, Mark: mark, Since: n.now(), SentAt: n.now()})
}

// ---- queue

func (n *Notifier) checkQueue() {
	msgs, err := n.checks.Queue()
	if err != nil {
		log.Printf("notify: read the queue: %v", err)
		return // no listing is no information
	}
	now := n.now()
	var old []postfix.QueuedMessage
	for _, m := range msgs {
		if now.Sub(m.Arrival) > QueueAge {
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
		if n.send("", queueStuck(n.hostname, old, now)) {
			n.save(store.NotifyState{Key: key, Phase: phaseOpen, Since: now, SentAt: now})
		}
	case len(old) == 0 && have:
		if n.send("", queueCleared(n.hostname, st.Since)) {
			n.forget(key)
		}
	}
}

// ---- level-2 limits

// CheckLimits looks for mail refused under a level-2 rate limit and reports
// each limit that refused, at most once in limitRepeat.
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
		if r.Domain == "" {
			continue
		}
		key := fmt.Sprintf("limit:%s:%s", r.Domain, r.AppLogin)
		st, have, err := n.st.GetNotifyState(key)
		if err != nil {
			log.Printf("notify: %v", err)
			continue
		}
		// Told within the day: the refusals counted now are the ones it was
		// told about, or more of the same. Told longer ago: everything in the
		// window is new since then.
		if have && now.Sub(st.SentAt) < limitRepeat {
			continue
		}
		if n.send(r.Domain, limitRefused(n.hostname, r)) {
			n.save(store.NotifyState{Key: key, Phase: phaseOpen, Since: r.First, SentAt: now})
		}
	}

	// A limit nobody has hit for two days is not a subject any more.
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
