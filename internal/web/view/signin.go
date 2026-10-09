package view

// The data of the two signed-out pages, sign-in and first-run setup. Neither has
// a user, so their Meta carries a Title and nothing else: Engine.Render then
// draws them with layout_signed_out, the split brand / form screen, and the
// page template supplies only the form column's content.

// Login is the data of the sign-in page (templates/login.html). Host is the
// name the panel is served as, shown above the title so the person knows which
// server is asking for a password; it is left out when the name is not known.
// SetupHint replaces the form by a pointer to the one-time setup link, for a
// panel that has no administrator yet. Error is the refusal of the last attempt
// ("Too many attempts…"); the page shows it as a flash above the fields.
type Login struct {
	Meta
	Host      string
	SetupHint bool
	Error     string
}

// NewLogin returns the sign-in page's data. host and formErr may be empty.
func NewLogin(host, formErr string) *Login {
	return &Login{Meta: Meta{Title: "Sign in"}, Host: host, Error: formErr}
}

// NewLoginSetupHint returns the sign-in page's data for a panel with no
// administrator yet: no form, only the way to the setup link.
func NewLoginSetupHint(host string) *Login {
	return &Login{Meta: Meta{Title: "Sign in"}, Host: host, SetupHint: true}
}

// Notice is the page's error as the flash partial's input; it is read only when
// Error is set.
func (l *Login) Notice() Flash { return Flash{Error: true, Text: Plain(l.Error)} }

// Setup is the data of the first-run page that creates the administrator
// (templates/setup.html), reached by the one-time link /setup/<Token>. Token is
// the path segment the form posts back to; Error is the refusal of the last
// submission, shown as a flash above the fields.
type Setup struct {
	Meta
	Token string
	Error string
}

// NewSetup returns the setup page's data. formErr may be empty.
func NewSetup(token, formErr string) *Setup {
	return &Setup{Meta: Meta{Title: "Create administrator"}, Token: token, Error: formErr}
}

// Notice is the page's error as the flash partial's input; it is read only when
// Error is set.
func (s *Setup) Notice() Flash { return Flash{Error: true, Text: Plain(s.Error)} }
