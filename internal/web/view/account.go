package view

// The data of Account (templates/account.html, mockup account.html) and of
// Server › Settings (templates/settings.html, mockup settings.html): what used
// to be one Settings page, split by whose it is. Account is the signed-in
// user's own — who they are to the panel and their password — and every role
// has it. Settings is the instance's.

import "strconv"

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
			Lead: Rich("What holds for the whole instance, whoever is signed in. Your own name, e-mail and password are under ",
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
