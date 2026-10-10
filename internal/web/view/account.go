package view

// The data of Account (templates/account.html, mockup account.html) and of
// Server › Settings (templates/settings.html, mockup settings.html): what used
// to be one Settings page, split by whose it is. Account is the signed-in
// user's own — who they are to the panel, their password and where DMARC reports
// about their domains go — and every role has it. Settings is the instance's.

import (
	"strconv"
	"strings"
)

// Account is the data of the Account page. Three forms, each its own POST: the
// profile (Username, Email), the password, and the default report address
// (DMARCSelected, DMARCAddress). The form fields hold what was stored, or what
// was just submitted when a form is shown again with an error.
type Account struct {
	Meta
	Head  Head
	Flash *Flash

	Profile  Box
	Username string
	Email    string

	Password Box

	DMARC         Box
	DMARCOptions  []Option
	DMARCSelected string
	DMARCAddress  string
	DMARCHelp     Text
	hosted        bool
	// Authorization is the report-authorization record the default address
	// needs when it is on another domain than the senders; nil when it needs
	// none (the hosted address, no reports, an address on the senders' domain).
	Authorization *Record
}

// DMARC default choices, the values of the select (store.DMARCDefault*).
const (
	DMARCChoiceHosted  = "hosted"
	DMARCChoiceAccount = "account"
	DMARCChoiceCustom  = "custom"
	DMARCChoiceNone    = "none"
)

// NewAccount builds the page for a user. role is "global" or "domain" and
// appears in the kicker. hosted says whether SelfPost-hosted reports are
// offered (report ingest is on), accountEmail is the e-mail the "my account
// e-mail" choice names.
func NewAccount(m Meta, role string, hosted bool, accountEmail string) *Account {
	m.Title, m.Section, m.Page = "Account", "user", "account"
	a := &Account{
		Meta: m,
		Head: Head{
			Kicker: "Signed in as " + m.User + " · " + role,
			Title:  "Account",
			Lead:   Plain("Who you are to this panel and where reports about your domains go. Applications keep their own logins and passwords and are not affected."),
		},
		Profile:  Box{No: "01", Title: "Profile"},
		Password: Box{No: "02", Title: "Password"},
		DMARC: Box{No: "03", Title: "DMARC reports", ID: "dmarc",
			Help: &HelpLink{Href: "/help#" + HelpDNS, Title: "DMARC and where its reports go"}},
		hosted: hosted,
	}
	a.WithDomainUse(0, 0)
	if hosted {
		a.DMARCOptions = append(a.DMARCOptions, Option{Value: DMARCChoiceHosted, Label: "SelfPost hosted — read them under DMARC reports"})
	}
	account := "My account e-mail"
	if accountEmail != "" {
		account += " — " + accountEmail
	}
	a.DMARCOptions = append(a.DMARCOptions,
		Option{Value: DMARCChoiceAccount, Label: account},
		Option{Value: DMARCChoiceCustom, Label: "Another address…"},
		Option{Value: DMARCChoiceNone, Label: "No reports"},
	)
	return a
}

// WithDomainUse says how many of the user's outbound domains follow their
// default address now (of total); nothing is said when they have none.
func (a *Account) WithDomainUse(following, total int) *Account {
	help := Rich("Used by every outbound domain whose report address is set to ", Em(a.Meta.User+"'s default"))
	if total > 0 {
		help = append(help, Inline{Text: " — " + strconv.Itoa(following) + " of " + strconv.Itoa(total) + " now"})
	}
	help = append(help, Inline{Text: ". Changing it changes the DMARC record those domains must publish. "})
	if a.hosted {
		help = append(help, Inline{Text: "The hosted address is parsed by SelfPost; any other just receives the raw XML."})
	} else {
		help = append(help, Inline{Text: "The address receives the raw XML."})
	}
	a.DMARCHelp = help
	return a
}

// WithResult shows the result of the last action, or its refusal, between the
// head and the first box.
func (a *Account) WithResult(flash, formErr string) *Account {
	switch {
	case formErr != "":
		a.Flash = &Flash{Error: true, Text: Plain(formErr)}
	case flash != "":
		a.Flash = &Flash{Text: Plain(flash)}
	}
	return a
}

// WithAuthorization adds the report-authorization record the default address
// needs on its own domain: where to publish it (host, value) and what DNS said
// about it — a status ("ok", "warn", "error" or "unknown"), the sentence that
// explains it and the records found. The tag in the box head repeats the
// verdict.
func (a *Account) WithAuthorization(host, value, status, detail string, found []string) *Account {
	rec := &Record{
		Name: "Report authorization",
		Note: Plain("Needed because the address is on another domain than the senders"),
		Host: host, Type: "TXT", Value: value,
	}
	switch status {
	case "ok":
		rec.Status = Tag{Status: "published"}
		a.DMARC.End = Rich(TagOf("ok", "authorized"))
	case "warn", "error":
		level := map[string]string{"warn": "warn", "error": "fail"}[status]
		rec.Status = Tag{Status: level, Label: "not published"}
		if len(found) > 0 {
			rec.InDNS = strings.Join(found, "  ")
			rec.Problem = Plain(detail)
		} else {
			rec.Note = Plain(detail)
		}
		a.DMARC.End = Rich(TagOf(level, "not authorized"))
	case "unknown":
		rec.Status = Tag{Status: "unknown", Label: "not checked"}
		rec.Note = Plain(detail)
		a.DMARC.End = Rich(TagOf("unknown", "not checked"))
	}
	a.Authorization = rec
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

	RateLimits Box
	RateFacts  []Fact
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
			Lead: Rich("What holds for the whole instance, whoever is signed in. Your own name, e-mail and DMARC report address are under ",
				Link("/account", "Account"), "."),
		},
		Retention:     Box{No: "01", Title: "Log retention", Help: topicLink(HelpSettings, "How long rows are kept")},
		RetentionDays: retention,
		RateLimits:    Box{No: "02", Title: "Sending rate limits", ID: "rate-limits", End: Plain("Read-only"), Help: topicLink(HelpLimits, "Levels 1 and 2")},
		RateFacts: []Fact{
			{Label: "Level 1 · per client IP", Value: Plain(level1), Big: true,
				Note: Rich("Set in ", Code(".env"), "; restart to change. Nothing below may exceed it.")},
			{Label: "Level 2 · domain, application",
				Value: Rich("Set on each ", Link("/outbound/domains", "domain"), " and on each application of a domain.")},
		},
	}
}

// WithResult shows the result of the last action, or its refusal, between the
// head and the form.
func (s *Settings) WithResult(flash, formErr string) *Settings {
	switch {
	case formErr != "":
		s.Flash = &Flash{Error: true, Text: Plain(formErr)}
	case flash != "":
		s.Flash = &Flash{Text: Plain(flash)}
	}
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
