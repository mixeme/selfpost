package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// message is one notification: a subject line and a plain-text body.
//
// The texts name the page of the panel to open rather than link to it: the
// panel does not know its own address - it sits behind the operator's reverse
// proxy - and a link built from a guess would be a wrong link in a message
// people are asked to trust.
type message struct {
	subject string
	body    string
}

// maxListed is how many messages of the queue a notification names. The rest
// are counted: the page shows them all.
const maxListed = 10

func compose(hostname, subject string, paragraphs ...string) message {
	paragraphs = append(paragraphs,
		fmt.Sprintf("Sent by the SelfPost panel on %s. You receive this because notifications are on in your Account there; turn them off on the same page.", hostname))
	return message{
		subject: "[SelfPost] " + subject,
		body:    strings.Join(paragraphs, "\n\n") + "\n",
	}
}

func stamp(t time.Time) string { return t.Local().Format("2006-01-02 15:04 MST") }

// plain makes text that came from outside fit for a message body: a queued
// message's sender is whatever its sender wrote, and a DNS finding may quote a
// name. What is not printable ASCII becomes "?", and it is cut to max bytes -
// a notification is read by a person, and nothing in it is theirs to act on
// but the panel's own words.
func plain(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	b := []byte(s)
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}

// days words a duration in days, the unit these messages count in, rounded
// to the nearest: a certificate found thirteen days and twenty-three hours
// from its end has fourteen days, not thirteen.
func days(d time.Duration) string {
	n := int((d + 12*time.Hour) / (24 * time.Hour))
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func dnsProblem(hostname, domain string, faults []dnsFault, since time.Time) message {
	names := make([]string, len(faults))
	lines := make([]string, len(faults))
	for i, f := range faults {
		names[i] = f.Record
		lines[i] = f.Record + ": " + plain(f.Detail, 600)
	}
	return compose(hostname,
		fmt.Sprintf("%s: %s does not match", domain, strings.Join(names, ", ")),
		fmt.Sprintf("The DNS records of %s do not match what this server sends with. Checks about an hour apart found the same, the first at %s.", domain, stamp(since)),
		strings.Join(lines, "\n\n"),
		fmt.Sprintf("Until the records are right, receivers may refuse mail from %s or file it as spam. The records to publish are in the panel under Outbound > Domains > %s.", domain, domain),
		"One more message follows when the records match again.",
	)
}

func dnsCleared(hostname, domain string, since time.Time) message {
	return compose(hostname,
		fmt.Sprintf("%s: DNS records match again", domain),
		fmt.Sprintf("The DNS records of %s match what this server sends with again; checks about an hour apart agree. They had not since %s.", domain, stamp(since)),
	)
}

func certExpiring(hostname string, notAfter time.Time, left time.Duration) message {
	subject := fmt.Sprintf("TLS certificate of %s expires in %s", hostname, days(left))
	first := fmt.Sprintf("The TLS certificate this server presents on its submission ports expires on %s, in %s.", stamp(notAfter), days(left))
	if left <= 0 {
		subject = fmt.Sprintf("TLS certificate of %s has expired", hostname)
		first = fmt.Sprintf("The TLS certificate this server presents on its submission ports expired on %s.", stamp(notAfter))
	}
	return compose(hostname, subject,
		first,
		"Once it has expired, applications that verify the certificate can no longer submit mail. Renewal normally happens on the host, at the reverse proxy the certificate comes from; check that it still runs. The panel shows the certificate under Server > Health.",
	)
}

func queueStuck(hostname string, old []postfix.QueuedMessage, now time.Time) message {
	var b strings.Builder
	for i, m := range old {
		if i == maxListed {
			fmt.Fprintf(&b, "... and %d more\n", len(old)-maxListed)
			break
		}
		sender := m.Sender
		if sender == "" {
			sender = "(bounce)"
		}
		fmt.Fprintf(&b, "%s  from %s  waiting %s, since %s\n", plain(m.ID, 24), plain(sender, 120), days(now.Sub(m.Arrival)), stamp(m.Arrival))
	}
	what := "A message has"
	if len(old) > 1 {
		what = fmt.Sprintf("%d messages have", len(old))
	}
	return compose(hostname,
		fmt.Sprintf("%s been in the queue of %s for more than %s", what, hostname, days(QueueAge)),
		fmt.Sprintf("%s waited in the mail queue for more than %s. Postfix goes on retrying and returns a message to its sender when the queue lifetime runs out.", what, days(QueueAge)),
		strings.TrimRight(b.String(), "\n"),
		"The reason the receiving server gives is in the panel: Outbound > Queue lists what is held, and the message's page in Outbound > Log shows what Postfix logged about it.",
		"One more message follows when nothing that old is left in the queue.",
	)
}

func queueCleared(hostname string, since time.Time) message {
	return compose(hostname,
		fmt.Sprintf("The queue of %s holds nothing older than %s", hostname, days(QueueAge)),
		fmt.Sprintf("No message in the mail queue is older than %s any more: what was waiting since the notification of %s was delivered, or returned to its sender.", days(QueueAge), stamp(since)),
	)
}

func limitRefused(hostname string, r store.LimitRefusal) message {
	who := fmt.Sprintf("the application %s of %s", strings.Join(r.Apps, ", "), r.Domain)
	if len(r.Apps) > 1 {
		who = fmt.Sprintf("the applications %s of %s", strings.Join(r.Apps, ", "), r.Domain)
	}
	count := "1 message"
	if r.Count != 1 {
		count = fmt.Sprintf("%d messages", r.Count)
	}
	return compose(hostname,
		fmt.Sprintf("%s: a rate limit is refusing mail", r.Domain),
		fmt.Sprintf("In the last 24 hours this server refused %s from %s under a level-2 rate limit, the first at %s. Refused mail is not queued: the application has to send it again.", count, who, stamp(r.First)),
		fmt.Sprintf("If the volume is expected, raise the limit in the panel: Outbound > Domains > %s, on the domain or on the application. If it is not, the application is sending more than it should.", r.Domain),
		"Refusals of this domain's mail are reported at most once a day.",
	)
}
