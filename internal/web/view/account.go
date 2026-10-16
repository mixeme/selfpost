package view

// The data of Account (templates/account.html, mockup account.html) and of
// Server › Settings (templates/settings.html, mockup settings.html): what used
// to be one Settings page, split by whose it is. Account is the signed-in
// user's own — who they are to the panel and their password — and every role
// has it. Settings is the instance's.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Account is the data of the Account page. Two forms, each its own POST: the
// profile (Username, Email) and the password. The fields hold what was stored,
// or what was just submitted when a form is shown again with an error.
type Account struct {
	Meta
	Head  Head
	Flash *Flash

	Profile  Box
	Username string
	Email    string
	// Notify is whether the panel writes to this user: the checkbox under the
	// e-mail.
	Notify bool

	Password Box
}

// NewAccount builds the page for a user. role is "global" or "domain" and
// appears in the kicker.
func NewAccount(m Meta, role string) *Account {
	m.Title, m.Section, m.Page = "Account", "user", "account"
	return &Account{
		Meta: m,
		Head: Head{
			Kicker: "Signed in as " + m.User + " · " + role,
			Title:  "Account",
			Lead:   Plain("Who you are to this panel. Applications keep their own logins and passwords and are not affected."),
		},
		Profile:  Box{No: "01", Title: "Profile"},
		Password: Box{No: "02", Title: "Password"},
	}
}

// WithResult shows the result of the last action, or its refusal, between the
// head and the first box.
func (a *Account) WithResult(flash, formErr string) *Account {
	a.Flash = resultFlash(flash, formErr)
	return a
}

// Settings is the data of Server › Settings: one form with the instance's
// settings and one Save settings button, and the read-only level-1 rate limit
// the operator sets in .env.
type Settings struct {
	Meta
	Head  Head
	Flash *Flash

	Retention     Box
	RetentionDays string
	// Inbound is whether the inbound relay is on, which gives the retention box
	// its second field; InboundRetentionDays is what that field shows.
	Inbound              bool
	InboundRetentionDays string

	RateLimits Box
	RateFacts  []Fact

	// Notifications is the instance switch for notification e-mails; Notify is
	// its state and NotifyNote the line under it, which says from where and to
	// whom.
	Notifications Box
	Notify        bool
	NotifyNote    Text

	// Mail holds the DNS records of the server's own name, which notifications
	// are signed and authorised with, drawn like a domain's.
	Mail        Box
	MailRecords []Record

	// TimeZone names the container's zone, which every time and day the
	// panel shows is in; it is set in .env, so the box only shows it.
	TimeZone  Box
	ZoneFacts []Fact
}

// NewSettings builds the page. retention is what the field shows (the stored
// value, or what was just submitted); level1 is the per-client-IP limit as
// FormatRate writes it.
func NewSettings(m Meta, retention, level1 string) *Settings {
	m.Title, m.Section, m.Page = "Settings", "server", "settings"
	return &Settings{
		Meta: m,
		Head: Head{
			Kicker: "Server",
			Title:  "Settings",
			Lead: Rich("What holds for the whole instance, whoever is signed in. Your own name, e-mail and password are under ",
				Link("/account", "Account"), "."),
			Actions: []Action{{Label: "Re-check DNS", Icon: "ti-refresh", Post: settingsDNSRecheck}},
		},
		Retention:     Box{No: "01", Title: "Log retention", Help: topicLink(HelpSettings, "How long rows are kept")},
		RetentionDays: retention,
		RateLimits:    Box{No: "04", Title: "Sending rate limits", ID: "rate-limits", End: Plain("Read-only"), Help: topicLink(HelpLimits, "Levels 1 and 2")},
		RateFacts: []Fact{
			{Label: "Level 1 · per client IP", Value: Plain(level1), Big: true,
				Note: Rich("Set in ", Code(".env"), "; restart to change. Nothing below may exceed it.")},
			{Label: "Level 2 · domain, application",
				Value: Rich("Set on each ", Link("/outbound/domains", "domain"), " and on each application of a domain.")},
		},
		Notifications: Box{No: "02", Title: "Notifications", ID: "notifications", Help: topicLink(HelpNotifications, "What is sent and when")},
		Mail: Box{No: "03", Title: "Notification mail", ID: "notification-mail", End: Plain("DNS records of the server's own name"),
			Help: topicLink(HelpNotifications, "What the panel sends and what makes it arrive")},
		TimeZone: Box{No: "05", Title: "Time zone", ID: "time-zone", End: Plain("Read-only"), Help: topicLink(HelpSettings, "Times and days of the panel")},
	}
}

// WithNotifications sets the instance switch: on is whether notification
// e-mails are sent at all, from is the address they come from.
func (s *Settings) WithNotifications(on bool, from string) *Settings {
	s.Notify = on
	s.NotifyNote = Rich("From ", Mono(from), ", to every user who has an e-mail and has not turned them off under ",
		Link("/account", "Account"), ".")
	return s
}

// settingsDNSRecheck is where Re-check DNS on Settings posts.
const settingsDNSRecheck = "/server/settings/dns-recheck"

// WithMail sets the records of the server's own name, in the order they are
// shown (ServerMailDKIM, ServerMailSPF), and how long ago they were checked
// (FormatAge; "" when they were not).
func (s *Settings) WithMail(records []Record, checked string) *Settings {
	s.MailRecords = records
	s.Mail.End = Plain("DNS records of the server's own name")
	if checked != "" {
		s.Mail.End = Plain("The server's own name · checked " + checked)
	}
	return s
}

// WithTimeZone names the zone: tz is TZ as the container has it ("" is UTC,
// Go's and the C library's default), loc the zone the process took from it
// (time.Local), at now for its offset. A TZ Go cannot load leaves time.Local
// UTC (time/zoneinfo_unix.go, initLocal), and the box says so rather than name
// a zone the times are not in.
func (s *Settings) WithTimeZone(tz string, loc *time.Location, now time.Time) *Settings {
	name := strings.TrimPrefix(tz, ":")
	if name == "" {
		name = "UTC"
	}
	_, offset := now.In(loc).Zone()
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	note := Rich(fmt.Sprintf("UTC%s%02d:%02d. Set in ", sign, offset/3600, offset%3600/60), Code(".env"), " (", Code("TZ"), "); restart to change.")
	if name != "UTC" && loc.String() != name {
		note = Rich("Not a zone this image knows, so the times are UTC. Set in ", Code(".env"), " (", Code("TZ"), "); restart to change.")
	}
	s.ZoneFacts = []Fact{{Label: "Zone", Value: Plain(name), Mono: true, Note: note}}
	return s
}

// WithInbound adds the retention of the Inbound log to the Log retention box.
// days is what the field shows, like retention in NewSettings. The field exists
// only while the inbound relay is on: with it off there is no journal to keep.
func (s *Settings) WithInbound(days string) *Settings {
	s.Inbound, s.InboundRetentionDays = true, days
	return s
}

// WithResult shows the result of the last action, or its refusal, between the
// head and the form.
func (s *Settings) WithResult(flash, formErr string) *Settings {
	s.Flash = resultFlash(flash, formErr)
	return s
}

// FormatRate writes a rate limit of messages per window the short way:
// "600 / h" for an hourly limit, "30 / min", "5 000 / day", and the window in
// seconds for any other.
func FormatRate(messages, windowSeconds int) string {
	n := FormatCount(int64(messages))
	switch {
	case windowSeconds == 60:
		return n + " / min"
	case windowSeconds == 3600:
		return n + " / h"
	case windowSeconds == 86400:
		return n + " / day"
	case windowSeconds > 0 && windowSeconds%3600 == 0:
		return n + " / " + strconv.Itoa(windowSeconds/3600) + " h"
	}
	return n + " / " + strconv.Itoa(windowSeconds) + " s"
}
