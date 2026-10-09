package view

// The data of the Server pages that are not a health table: the system log
// (templates/system_log.html, mockup system-log.html), the backup and the
// domain import (backup.html, backup.html), the users list (users.html,
// users.html), the user form (user.html, user.html) and the confirmation before
// a user is deleted (user_delete.html, user-delete.html). Everything under
// /server/ is for the global role.
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads the stores, the view decides what is said
// and how it is worded.

import (
	"strconv"
	"strings"
)

const (
	serverLog    = "/server/log"
	serverBackup = "/server/backup"
	serverUsers  = "/server/users"
)

// UserHref is the address of a panel user's form.
func UserHref(id int64) string { return serverUsers + "/" + strconv.FormatInt(id, 10) }

// ---- System log

// SystemLog is the data of the system log: the tail of mail.log, newest line
// first, in the one box that polls. The page and its fragment (GET
// /server/log/fragment) render the same box (templates/system_log_body.html).
type SystemLog struct {
	Meta
	Head Head

	Log   Box
	Lines []LogLine
	// Empty is shown in place of the lines when there are none: the log has
	// nothing in it yet, or it could not be read.
	Empty EmptyState
}

// NewSystemLog builds the page. lines are the log's tail as it was read, oldest
// first; the page shows them newest first. errText is why the log could not be
// read, and empty when it could.
func NewSystemLog(m Meta, lines []LogLine, errText string) *SystemLog {
	m.Title, m.Section, m.Page = "System log", "server", "log"
	p := &SystemLog{
		Meta: m,
		Head: Head{
			Kicker: "Server",
			Title:  "System log",
			Lead:   Rich("The tail of ", Code("mail.log"), " — Postfix, OpenDKIM and the milters. For one message, open it from the ", Link(outboundLog, "Outbound log"), " instead."),
		},
		Log: Box{No: "01", Title: "Recent lines", ID: "system-log", Poll: serverLog + "/fragment",
			End: Plain("Last 200 lines · refreshes on its own")},
		Empty: EmptyState{Icon: "ti-file-text", Text: Plain("No log lines yet.")},
	}
	for i := len(lines) - 1; i >= 0; i-- {
		p.Lines = append(p.Lines, lines[i])
	}
	if errText != "" {
		p.Empty = EmptyState{Icon: "ti-alert-circle", Text: Plain(errText)}
	}
	return p
}

// ---- Backup

// Backup is the data of the backup page: the whole instance in one archive, and
// the import of one domain from another SelfPost. Both files are secrets and
// both can be sealed with a password, so the page carries the password fields of
// the full backup; the import asks for one only when the file needs it.
type Backup struct {
	Meta
	Head  Head
	Flash *Flash

	Full         Box
	FullAction   string
	Encrypt      bool
	PasswordMin  int
	Import       Box
	ImportAction string
}

// NewBackup builds the page. The encryption box is ticked, as on the domain
// export: a secret file is sealed unless the operator chooses otherwise, and
// after a refusal the page comes back the same way. refusal is the message of
// whichever form was turned down, empty on a first visit.
func NewBackup(m Meta, passwordMin int, refusal string) *Backup {
	m.Title, m.Section, m.Page = "Backup", "server", "backup"
	p := &Backup{
		Meta: m,
		Head: Head{
			Kicker: "Server",
			Title:  "Backup",
			Lead:   Plain("Take the whole instance with you, or bring one domain in from another SelfPost. Both files are secrets."),
		},
		Full:         Box{No: "01", Title: "Full backup"},
		FullAction:   serverBackup,
		Encrypt:      true,
		PasswordMin:  passwordMin,
		Import:       Box{No: "02", Title: "Import a domain", ID: "import"},
		ImportAction: serverBackup + "/import",
	}
	if refusal != "" {
		p.Flash = &Flash{Error: true, Text: Plain(refusal)}
	}
	return p
}

// ---- Users

// Users is the data of the list of panel users.
type Users struct {
	Meta
	Head  Head
	Flash *Flash

	List        Box
	Rows        []UserRow
	ShowInbound bool
}

// UserRow is one panel user as the list shows them: the name, with a mark on
// the signed-in user's own row, the address, the role as a tag, the domains of
// each direction as words and the way to the form.
type UserRow struct {
	Name     string
	You      bool
	Email    string
	Role     Tag
	Outbound string
	Inbound  string
	Href     string
}

// UserRowInput is what the list needs to know of one user. A global user
// reaches every domain, so the lists of names are only read for the domain role.
type UserRowInput struct {
	ID          int64
	Username    string
	Email       string
	Global      bool
	You         bool
	AllOutbound bool
	AllInbound  bool
	Outbound    []string
	Inbound     []string
}

// NewUserRow words one user of the list.
func NewUserRow(in UserRowInput) UserRow {
	role := Tag{Status: "domain"}
	if in.Global {
		role = Tag{Status: "global"}
	}
	return UserRow{
		Name: in.Username, You: in.You, Email: orDash(in.Email), Role: role,
		Outbound: reachWords(in.Global || in.AllOutbound, in.Outbound),
		Inbound:  reachWords(in.Global || in.AllInbound, in.Inbound),
		Href:     UserHref(in.ID),
	}
}

// YouTag is the mark on the signed-in user's own row.
func (r UserRow) YouTag() Tag { return Tag{Label: "you"} }

// reachWords is what the list says of one direction of a user's reach: All, the
// names assigned, or a dash where nothing is.
func reachWords(all bool, names []string) string {
	switch {
	case all:
		return "All"
	case len(names) > 0:
		return strings.Join(names, ", ")
	}
	return dash
}

// NewUsers builds the page. inbound is whether the inbound feature is on, which
// decides whether the list has the column and the lead speaks of it; flash is
// the result of the last action.
func NewUsers(m Meta, inbound bool, flash string) *Users {
	m.Title, m.Section, m.Page = "Users", "server", "users"
	reach := Rich("the domains assigned", Inline{Text: "."})
	if inbound {
		reach = Rich("the domains assigned, outbound and inbound separately", Inline{Text: "."})
	}
	p := &Users{
		Meta: m,
		Head: Head{
			Kicker: "Server",
			Title:  "Users",
			Lead: Rich("Who may sign in to this panel. Everyone here is an administrator; the role is the reach — ",
				Em("global"), " is the whole server, ", Em("domain"), " is ", reach),
			Actions: []Action{{Label: "Create user", Icon: "ti-user-plus", Href: serverUsers + "/new", Primary: true}},
		},
		List:        Box{No: "01", Title: "Panel users"},
		ShowInbound: inbound,
	}
	if flash != "" {
		p.Flash = &Flash{Text: Plain(flash)}
	}
	return p
}

// WithRows sets the users and counts them in the box head.
func (p *Users) WithRows(rows []UserRow) *Users {
	p.Rows = rows
	p.List.End = Plain(plural(int64(len(rows)), "user", "users"))
	return p
}

// ---- The user form

// UserForm is the data of the form that creates a panel user and edits one: the
// account in one box, and the domains the user reaches, outbound and inbound
// separately, in the other column. The same form serves both; a blank password
// on an existing user means keep the current one.
type UserForm struct {
	Meta
	Head  Head
	Flash *Flash

	// Action is where the form is sent, Submit what its button says and
	// CancelHref where Cancel goes.
	Action     string
	Submit     string
	CancelHref string

	Account     Box
	Username    string
	Email       string
	NewUser     bool
	PasswordMin int
	Roles       []Option
	Role        string
	// RoleLocked is the only global user: the role cannot be changed, so the
	// select is read-only and the role travels in a hidden field.
	RoleLocked bool

	Outbound   Box
	AllOut     bool
	OutDomains []DomainChoice

	ShowInbound bool
	Inbound     Box
	AllIn       bool
	InDomains   []DomainChoice
}

// DomainChoice is one row of a list of domains on the user form.
type DomainChoice struct {
	ID      int64
	Name    string
	Checked bool
}

// UserFormInput is what a user form shows: a new user's blanks or an existing
// user's values, or what was typed into a form that was refused.
type UserFormInput struct {
	// ID is the user being edited, 0 for a new one; Name is that user's stored
	// name, which the head shows whatever was typed into the form.
	ID   int64
	Name string

	Username   string
	Email      string
	Role       string
	RoleLocked bool
	// CanDelete says whether the head offers Delete user.
	CanDelete bool

	PasswordMin int
	ShowInbound bool

	AllOut     bool
	OutDomains []DomainChoice
	AllIn      bool
	InDomains  []DomainChoice

	Error string
}

// NewUserForm builds the page.
func NewUserForm(m Meta, in UserFormInput) *UserForm {
	crumbs := []Crumb{{Href: serverUsers, Label: "Users"}}
	title := "Create user"
	if in.ID != 0 {
		title = in.Name
	}
	m.Title, m.Section, m.Page = title+" · Users", "server", "users"
	p := &UserForm{
		Meta:       m,
		Head:       Head{Crumbs: crumbs, Title: title},
		CancelHref: serverUsers,

		Account:     Box{No: "01", Title: "Account"},
		Username:    in.Username,
		Email:       in.Email,
		NewUser:     in.ID == 0,
		PasswordMin: in.PasswordMin,
		Roles: []Option{
			{Value: "domain", Label: "Domain — the domains assigned"},
			{Value: "global", Label: "Global — the whole server"},
		},
		Role:       in.Role,
		RoleLocked: in.RoleLocked,

		Outbound:    Box{No: "02", Title: "Outbound domains", End: Plain("DNS records, applications, log")},
		AllOut:      in.AllOut,
		OutDomains:  in.OutDomains,
		ShowInbound: in.ShowInbound,
		Inbound:     Box{No: "03", Title: "Inbound domains", End: Plain("Upstream, recipients, log")},
		AllIn:       in.AllIn,
		InDomains:   in.InDomains,
	}
	if p.NewUser {
		p.Action, p.Submit = serverUsers+"/new", "Create user"
	} else {
		p.Action, p.Submit = UserHref(in.ID), "Save user"
		if in.CanDelete {
			p.Head.Actions = []Action{{Label: "Delete user", Icon: "ti-trash", Href: UserHref(in.ID) + "/delete", Danger: true}}
		}
	}
	if in.Error != "" {
		p.Flash = &Flash{Error: true, Text: Plain(in.Error)}
	}
	return p
}

// ---- Delete a user

// UserDelete is the data of the confirmation before a panel user is deleted:
// what goes with them, the one button that does it, and the way out.
type UserDelete struct {
	Meta
	Head Head

	Danger       Box
	Intro        Text
	Consequences []Text
	Note         Text
	Action       string
	Confirm      string
	KeepHref     string
}

// UserDeleteInput is what the confirmation needs to know of the user.
type UserDeleteInput struct {
	ID       int64
	Username string
	Global   bool
	// What the user is assigned: All of a direction, or the names of its domains.
	AllOutbound bool
	AllInbound  bool
	Outbound    []string
	Inbound     []string
	// Following names the domains whose DMARC report address is this user's
	// default: with the user gone they have none.
	Following []string
}

// NewUserDelete builds the page.
func NewUserDelete(m Meta, in UserDeleteInput) *UserDelete {
	m.Title, m.Section, m.Page = "Delete "+in.Username, "server", "users"
	href := UserHref(in.ID)
	p := &UserDelete{
		Meta: m,
		Head: Head{
			Crumbs: []Crumb{{Href: serverUsers, Label: "Users"}, {Href: href, Label: in.Username}},
			Title:  "Delete " + in.Username,
		},
		Danger: Box{Icon: "ti-alert-triangle", Title: "This cannot be undone", Variant: BoxDanger},
		Intro:  Rich("Deleting the panel user ", Strong(in.Username), " will:"),
		Consequences: []Text{
			Plain("end any signed-in session of this user at once;"),
		},
		Note:     Plain("Applications and their passwords, upstreams and recipients are not affected."),
		Action:   href + "/delete",
		Confirm:  "Delete " + in.Username,
		KeepHref: href,
	}
	if in.Global {
		p.Consequences = append(p.Consequences, Plain("remove their global access to the server and to every domain."))
		return p.withFollowing(in.Following)
	}
	var out, inb Text
	switch {
	case in.AllOutbound:
		out = Text{Strong("all outbound domains")}
	default:
		out = names(in.Outbound)
	}
	switch {
	case in.AllInbound:
		inb = Text{Strong("all inbound domains")}
	default:
		inb = names(in.Inbound)
	}
	switch {
	case len(out) > 0 && len(inb) > 0:
		p.Consequences = append(p.Consequences, Rich("remove the assignments to ", out, " (outbound) and to ", inb, " (inbound)."))
	case len(out) > 0:
		p.Consequences = append(p.Consequences, Rich("remove the assignments to ", out, " (outbound)."))
	case len(inb) > 0:
		p.Consequences = append(p.Consequences, Rich("remove the assignments to ", inb, " (inbound)."))
	}
	return p.withFollowing(in.Following)
}

// followingShown is how many domain names the DMARC consequence spells out
// before it says "and N more".
const followingShown = 3

// withFollowing adds what deleting the user does to the domains that report to
// their default address: they are left with none. Nothing is added when no
// domain follows the user.
func (p *UserDelete) withFollowing(domains []string) *UserDelete {
	if len(domains) == 0 {
		return p
	}
	shown, more := domains, 0
	if len(domains) > followingShown {
		shown, more = domains[:followingShown], len(domains)-followingShown
	}
	var list Text
	for i, n := range shown {
		switch {
		case i == 0:
		case i == len(shown)-1 && more == 0:
			list = append(list, Inline{Text: " and "})
		default:
			list = append(list, Inline{Text: ", "})
		}
		list = append(list, Strong(n))
	}
	if more > 0 {
		list = append(list, Inline{Text: " and " + strconv.Itoa(more) + " more"})
	}
	p.Consequences = append(p.Consequences, Rich(list, " will have no DMARC report address until someone sets one."))
	return p
}

// names is a run of domain names, each strong, separated by commas.
func names(list []string) Text {
	var out Text
	for i, n := range list {
		if i > 0 {
			out = append(out, Inline{Text: ", "})
		}
		out = append(out, Strong(n))
	}
	return out
}
