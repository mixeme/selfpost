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
	// Info is the result of what brought the person back here - a reset link
	// asked for, a password set from one - shown as a success flash.
	Info string
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

// WithInfo sets the success flash.
func (l *Login) WithInfo(info string) *Login {
	l.Info = info
	return l
}

// InfoNotice is Info as the flash partial's input.
func (l *Login) InfoNotice() Flash { return Flash{Text: Plain(l.Info)} }

// Reset is the page that asks for a reset link (templates/login_reset.html,
// mockup login-reset.html): a username, and what will happen. Error is a
// refusal, or a link that no longer works.
type Reset struct {
	Meta
	Host  string
	Error string
}

// NewReset returns the reset request page's data.
func NewReset(host, formErr string) *Reset {
	return &Reset{Meta: Meta{Title: "Reset password"}, Host: host, Error: formErr}
}

// Notice is the page's error as the flash partial's input.
func (p *Reset) Notice() Flash { return Flash{Error: true, Text: Plain(p.Error)} }

// ResetSet is the page a reset link opens (templates/login_reset_set.html,
// mockup login-reset-set.html): the new password, for User, posted back to
// the link's own path.
type ResetSet struct {
	Meta
	Host  string
	User  string
	Token string
	Error string
}

// NewResetSet returns the new-password page's data.
func NewResetSet(host, user, token, formErr string) *ResetSet {
	return &ResetSet{Meta: Meta{Title: "New password"}, Host: host, User: user, Token: token, Error: formErr}
}

// Kicker is the host and the account the password is for.
func (p *ResetSet) Kicker() string { return kickerOf(p.Host, p.User) }

// Notice is the page's error as the flash partial's input.
func (p *ResetSet) Notice() Flash { return Flash{Error: true, Text: Plain(p.Error)} }

// Confirm is the page a confirmation link opens (templates/confirm_email.html,
// mockup confirm-email.html): the address and a button - a mail scanner
// opens a link, so the GET changes nothing. Done is the page after the
// button; Error a link that does not work, with no button.
type Confirm struct {
	Meta
	Host  string
	User  string
	Email string
	Token string
	Error string
	Done  bool
	// SignIn is the error of a link opened without its account's session:
	// the page points at sign-in.
	SignIn bool
}

// NewConfirm returns the confirmation page's data.
func NewConfirm(host, user, email, token string) *Confirm {
	return &Confirm{Meta: Meta{Title: "Confirm e-mail"}, Host: host, User: user, Email: email, Token: token}
}

// Kicker is the host and the account the address is of.
func (p *Confirm) Kicker() string { return kickerOf(p.Host, p.User) }

// Notice is the page's error as the flash partial's input.
func (p *Confirm) Notice() Flash { return Flash{Error: true, Text: Plain(p.Error)} }

// kickerOf joins the host and an account's name, either of which may be
// unknown.
func kickerOf(host, user string) string {
	switch {
	case host == "":
		return user
	case user == "":
		return host
	}
	return host + " · " + user
}

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
