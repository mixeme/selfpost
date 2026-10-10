package view

import (
	"regexp"
	"strings"
	"testing"
)

// ---- System log

// The page shows the newest line first, and its one box is the element that
// polls: the fragment answers with that same box.
func TestSystemLogShowsNewestFirstAndPollsItsBox(t *testing.T) {
	out := renderSignedIn(t, "system-log", systemLogFixture())
	pageHas(t, "System log", out,
		`<h1 class="title is-3">System log</h1>`, `<h2>Recent lines</h2>`, `Last 200 lines · refreshes on its own`,
		`id="system-log" data-poll aria-live="polite" hx-get="/server/log/fragment" hx-trigger="load" hx-swap="outerHTML"`,
		`<span class="sp-d">Sep 21 14:01:52</span> postfix/smtp[2131]`,
		`<span class="sp-w">postfix/smtp[2123]`, `<span class="sp-e">postfix/smtpd[2119]`,
	)
	if strings.Index(out, "postfix/smtp[2131]") > strings.Index(out, "opendkim[39]") {
		t.Error("the log is not shown newest line first")
	}
	// What a line says is escaped, not interpreted.
	if !strings.Contains(out, "to=&lt;m.keller@gmx.de&gt;") || strings.Contains(out, "<m.keller@gmx.de>") {
		t.Error("an address in a log line is not escaped")
	}

	frag := renderFragment(t, "system_log_body", systemLogFixture())
	if !strings.HasPrefix(frag, `<div class="box" id="system-log" data-poll`) || strings.Contains(frag, "sp-head") {
		t.Errorf("the fragment is not the polled box alone:\n%.200s", frag)
	}
	if strings.Count(out, `class="sp-log"`) != 1 || strings.Count(frag, `class="sp-log"`) != 1 {
		t.Error("the page and its fragment should each carry the log once")
	}
}

// With nothing to show, the box says why instead: nothing logged yet, or the
// log could not be read — and never both.
func TestSystemLogSaysWhyItHasNoLines(t *testing.T) {
	out := renderSignedIn(t, "system-log", NewSystemLog(admin(), nil, ""))
	pageHas(t, "System log", out, `No log lines yet.`, `class="sp-empty"`)
	pageLacks(t, "System log", out, `class="sp-log"`, `Could not read`)

	out = renderSignedIn(t, "system-log", NewSystemLog(admin(), nil, "Could not read the mail log."))
	pageHas(t, "System log", out, `Could not read the mail log.`, `ti-alert-circle`)
	pageLacks(t, "System log", out, `No log lines yet.`, `class="sp-log"`)
}

// ---- Backup

// Both forms keep what they were: the encryption fields and their script hooks,
// the multipart import and the anchor the domain page links to.
func TestBackupHasBothFormsAndTheEncryptionHooks(t *testing.T) {
	out := renderSignedIn(t, "backup", backupFixture())
	pageHas(t, "Backup", out,
		`action="/server/backup"`, `action="/server/backup/import" enctype="multipart/form-data"`,
		`name="encrypt" value="1"`, `data-encrypt-toggle`, `data-encrypt-fields`,
		`name="password"`, `name="password_confirm"`, `minlength="12"`, `placeholder="at least 12 characters"`,
		`name="file"`, `accept=".json,.spde,application/json"`, `required data-import-file`,
		`data-import-password-fields`, `name="import_password"`, `<div class="box" id="import">`,
	)
	pageLacks(t, "Backup", out, `notification`)
}

// The box is ticked on arrival, as on the domain export: the secret file is
// sealed unless the operator unticks it, and the page comes back the same way
// after a refusal, with the password fields in view.
func TestBackupEncryptionIsTickedLikeTheDomainExport(t *testing.T) {
	backup := renderSignedIn(t, "backup", backupFixture())
	export := renderSignedIn(t, "out-domain-settings", outDomainSettingsFixture())
	const box = `name="encrypt" value="1" checked data-encrypt-toggle>`
	pageHas(t, "Backup", backup, box)
	pageHas(t, "Domain settings", export, box)
	for _, hook := range []string{`data-encrypt-fields`, `name="password" type="password" autocomplete="new-password" minlength="12" placeholder="at least 12 characters"`,
		`name="password_confirm" type="password" autocomplete="new-password"`,
		`Keep this password: SelfPost does not store it, and the file cannot be opened without it.`} {
		pageHas(t, "Backup", backup, hook)
		pageHas(t, "Domain settings", export, hook)
	}
}

// A refusal is the flash above the boxes, with the encryption box still ticked.
func TestBackupRefusalIsTheFlashAndKeepsTheTick(t *testing.T) {
	out := renderSignedIn(t, "backup", NewBackup(admin(), 12, "The two passwords do not match."))
	pageHas(t, "Backup", out, `<div class="notification is-danger is-light">`, `The two passwords do not match.`, `name="encrypt" value="1" checked`)
	if strings.Index(out, `class="notification`) > strings.Index(out, `class="box"`) {
		t.Error("the flash is not above the first box")
	}
}

// ---- Users

func TestUsersListsTheReachOfEachUser(t *testing.T) {
	out := renderSignedIn(t, "users", usersFixture())
	pageHas(t, "Users", out,
		`<h1 class="title is-3">Users</h1>`, `<span class="sp-no">01</span><h2>Panel users</h2>`, `3 users`,
		`href="/server/users/new"`, `Create user`, `<div class="notification is-success is-light">`, `User created.`,
		`<strong>admin</strong> <span class="tag is-light">you</span>`,
		`<span class="tag is-primary is-light">global</span>`, `<span class="tag is-light">domain</span>`,
		`<th>Outbound domains</th><th>Inbound domains</th>`,
		`<td class="sp-muted">shop.example.org</td><td class="sp-muted">—</td>`,
		`<td class="sp-muted">All</td><td class="sp-muted">acme.io</td>`,
		`mix@<wbr>example.org`, `team@<wbr>shop.example.org`, `href="/server/users/3"`,
	)
	if strings.Count(out, `>you<`) != 1 {
		t.Error("only the signed-in user's own row carries the mark")
	}

	// Without the inbound feature the list has no inbound column, nor a lead
	// that speaks of one.
	p := NewUsers(admin(), false, "").WithRows([]UserRow{
		NewUserRow(UserRowInput{ID: 1, Username: "admin", Global: true, You: true}),
	})
	out = renderSignedIn(t, "users", p)
	pageHas(t, "Users", out, `1 user<`, `<th>Outbound domains</th><th></th>`)
	pageLacks(t, "Users", out, `<th>Inbound domains</th>`, `outbound and inbound separately`, `notification`)
	// A user who has not set an address is a dash, not a gap.
	pageHas(t, "Users", out, `<td class="sp-muted">—</td>`)
}

func TestReachWords(t *testing.T) {
	for _, tt := range []struct {
		all   bool
		names []string
		want  string
	}{
		{true, []string{"ignored.example"}, "All"},
		{false, []string{"a.example", "b.example"}, "a.example, b.example"},
		{false, nil, "—"},
	} {
		if got := reachWords(tt.all, tt.names); got != tt.want {
			t.Errorf("reachWords(%v, %v) = %q, want %q", tt.all, tt.names, got, tt.want)
		}
	}
	// A global user reaches everything whatever the lists say.
	row := NewUserRow(UserRowInput{ID: 1, Username: "root", Global: true})
	if row.Outbound != "All" || row.Inbound != "All" || row.Role.Status != "global" {
		t.Errorf("a global user's row = %+v", row)
	}
}

// ---- The user form

// The form for an existing user shows what is stored and sends everything the
// handler reads under the names it has always read it by. Saving it untouched
// sends the same values back: the role as selected, the All ticks as ticked, the
// single domains as ticked, and a blank password, which the handler reads as
// "keep".
func TestUserFormShowsWhatIsStoredUnderTheHandlersFieldNames(t *testing.T) {
	out := renderSignedIn(t, "user", userFixture())
	pageHas(t, "User", out,
		`<h1 class="title is-3">acme-ops</h1>`, `href="/server/users">Users</a>`, `href="/server/users/3/delete"`,
		`<form class="sp-form" method="post" action="/server/users/3">`,
		`name="username" value="acme-ops"`, `<label class="label" for="email">E-mail</label>`,
		`<input class="input" id="email" name="email" type="email" value="ops@acme.io" autocomplete="off">`,
		`name="password" type="password" autocomplete="new-password">`,
		`Leave empty to keep the current one.`,
		`<select id="role" name="role" data-global-role="global">`, `<option value="domain" selected>`, `<option value="global">`,
		`name="all_domains" value="1" checked`, `name="domain_ids" value="1">`, `name="domain_ids" value="3">`,
		`name="all_inbound_domains" value="1">`, `name="inbound_domain_ids" value="1">`, `name="inbound_domain_ids" value="2" checked`,
		`>Save user</button>`, `href="/server/users">Cancel</a>`,
	)
	pageLacks(t, "User", out, `name="password" type="password" autocomplete="new-password" required`, `disabled`,
		`notification`, `type="hidden"`)
	if strings.Count(out, ` checked`) != 2 {
		t.Errorf("exactly the stored ticks are checked, got %d", strings.Count(out, ` checked`))
	}
}

func TestUserFormForANewUser(t *testing.T) {
	p := NewUserForm(admin(), UserFormInput{Role: "domain", PasswordMin: 12, ShowInbound: true,
		OutDomains: []DomainChoice{{ID: 1, Name: "example.org"}}})
	out := renderSignedIn(t, "user", p)
	pageHas(t, "User", out,
		`<h1 class="title is-3">Create user</h1>`, `action="/server/users/new"`, `At least 12 characters.`,
		`autocomplete="new-password" required>`, `>Create user</button>`, `<option value="domain" selected>`)
	pageLacks(t, "User", out, `/delete`, `Leave empty to keep`, ` checked`, `ops@acme.io`)
	pageHas(t, "User", out, `name="email" type="email" value=""`)
}

// The only global user cannot be demoted: the select is read-only and the role
// travels in a hidden field, so a save sends it. The head does not offer Delete.
func TestUserFormOfTheOnlyGlobalUser(t *testing.T) {
	p := NewUserForm(admin(), UserFormInput{ID: 1, Name: "admin", Username: "admin", Role: "global", RoleLocked: true,
		PasswordMin: 12, ShowInbound: true})
	out := renderSignedIn(t, "user", p)
	pageHas(t, "User", out, `<input type="hidden" name="role" value="global">`, `<select id="role" name="role" data-global-role="global" disabled>`,
		`<option value="global" selected>`)
	pageLacks(t, "User", out, `/delete`, `Delete user`)
}

// The role select names the value that reaches every domain, and each list of
// domains is marked, so panel.js can hide the lists while the role is Global.
func TestUserFormMarksTheListsTheGlobalRoleHides(t *testing.T) {
	out := renderSignedIn(t, "user", userFixture())
	pageHas(t, "User", out, `<select id="role" name="role" data-global-role="global">`)
	if n := strings.Count(out, `<div class="box" data-domain-pick>`); n != 2 {
		t.Errorf("%d boxes are marked data-domain-pick, want the outbound and the inbound list", n)
	}
	out = renderSignedIn(t, "user", NewUserForm(admin(), UserFormInput{ID: 2, Name: "shop-team", Username: "shop-team", Role: "domain", PasswordMin: 12}))
	if n := strings.Count(out, "data-domain-pick"); n != 1 {
		t.Errorf("%d boxes are marked without the inbound feature, want the outbound list alone", n)
	}
}

// Inbound domains are offered only where the feature is on; the handler keeps
// what the user already has there when the form has no such list.
func TestUserFormHasNoInboundListWhenTheFeatureIsOff(t *testing.T) {
	p := NewUserForm(admin(), UserFormInput{ID: 2, Name: "shop-team", Username: "shop-team", Role: "domain", PasswordMin: 12})
	out := renderSignedIn(t, "user", p)
	pageHas(t, "User", out, `<h2>Outbound domains</h2>`)
	pageLacks(t, "User", out, `Inbound domains`, `all_inbound_domains`, `inbound_domain_ids`)
}

// A form that was refused says so above the boxes, shows what was typed, and
// names the user in the head as stored, not as typed.
func TestUserFormRefusalKeepsWhatWasTyped(t *testing.T) {
	p := NewUserForm(admin(), UserFormInput{ID: 3, Name: "acme-ops", Username: "<b>x</b>", Email: `a"b@x`, Role: "global",
		PasswordMin: 12, Error: "That username is already in use."})
	out := renderSignedIn(t, "user", p)
	pageHas(t, "User", out, `<div class="notification is-danger is-light">`, `That username is already in use.`,
		`<h1 class="title is-3">acme-ops</h1>`, `value="&lt;b&gt;x&lt;/b&gt;"`, `value="a&#34;b@x"`, `<option value="global" selected>`)
	if strings.Index(out, `class="notification`) > strings.Index(out, `<form class="sp-form"`) {
		t.Error("the flash is not above the form")
	}
}

// ---- Delete a user

func TestUserDeleteNamesWhatGoesWithTheUser(t *testing.T) {
	out := renderSignedIn(t, "user-delete", userDeleteFixture())
	pageHas(t, "Delete user", out,
		`<h1 class="title is-3">Delete acme-ops</h1>`, `href="/server/users">Users</a>`, `href="/server/users/3">acme-ops</a>`,
		`<div class="box sp-danger-zone">`, `This cannot be undone`,
		`end any signed-in session of this user at once;`,
		`remove the assignments to <strong>all outbound domains</strong> (outbound) and to <strong>acme.io</strong> (inbound).`,
		`<form method="post" action="/server/users/3/delete">`, `Delete acme-ops</button>`, `href="/server/users/3">Keep it</a>`,
	)

	for _, tt := range []struct {
		name string
		in   UserDeleteInput
		want string
	}{
		{"global", UserDeleteInput{ID: 1, Username: "root", Global: true},
			`remove their global access to the server and to every domain.`},
		{"named domains", UserDeleteInput{ID: 2, Username: "u", Outbound: []string{"a.example", "b.example"}, AllInbound: true},
			`remove the assignments to <strong>a.example</strong>, <strong>b.example</strong> (outbound) and to <strong>all inbound domains</strong> (inbound).`},
		{"inbound only", UserDeleteInput{ID: 2, Username: "u", Inbound: []string{"in.example"}},
			`remove the assignments to <strong>in.example</strong> (inbound).`},
	} {
		got := renderSignedIn(t, "user-delete", NewUserDelete(admin(), tt.in))
		if !strings.Contains(got, tt.want) {
			t.Errorf("%s: missing %q in\n%s", tt.name, tt.want, got)
		}
	}
}

// ---- Help

var (
	helpLink  = regexp.MustCompile(`class="sp-help" href="/help#([a-z-]+)"`)
	helpTopic = regexp.MustCompile(`class="sp-help-topic" id="([a-z-]+)"`)
)

// menuOnlyTopics are the topics of the Help page that no box of a fixture page
// links to, each with the reason it is reached from the menu alone. The rest of
// the Help tests are in help_test.go.
var menuOnlyTopics = map[string]string{
	HelpAccount: "the Account page is its own topic: its boxes (profile, password) explain themselves, so no box leads to it",
}

// A box's Help link is only worth having if the Help page has the topic it
// names, and a topic is only worth having if something leads to it. Every page
// of the panel is rendered with its fixture; every anchor it links to must be a
// topic of the Help page, and every topic must be linked from a page, except
// those in menuOnlyTopics.
func TestEveryHelpLinkLandsOnATopic(t *testing.T) {
	help := renderSignedIn(t, "help", NewHelp(admin(), true))
	linked := map[string][]string{}
	for name, fixture := range pageFixtures {
		for _, m := range helpLink.FindAllStringSubmatch(renderSignedIn(t, name, fixture()), -1) {
			linked[m[1]] = append(linked[m[1]], name)
		}
	}
	if len(linked) == 0 {
		t.Fatal("no page links to a Help topic, so nothing was checked")
	}
	topics := map[string]bool{}
	for _, m := range helpTopic.FindAllStringSubmatch(help, -1) {
		topics[m[1]] = true
	}
	for anchor, pages := range linked {
		if !topics[anchor] {
			t.Errorf("%v link to /help#%s, which is not a topic of the Help page", pages, anchor)
		}
	}
	for topic := range topics {
		reason, menuOnly := menuOnlyTopics[topic]
		switch {
		case len(linked[topic]) == 0 && !menuOnly:
			t.Errorf("Help topic %q is linked from no page: link it from the box it explains, or list it in menuOnlyTopics with the reason", topic)
		case len(linked[topic]) > 0 && menuOnly:
			t.Errorf("Help topic %q is listed as menu-only (%s) but %v link to it", topic, reason, linked[topic])
		}
	}
	for topic := range menuOnlyTopics {
		if !topics[topic] {
			t.Errorf("menuOnlyTopics names %q, which is not a topic of the Help page", topic)
		}
	}
}

// The e-mail help says only what is so: it is the user's own address, they can
// change it under Account, and it can receive DMARC reports if they choose.
func TestUserFormEmailHelpSaysOnlyWhatIsTrue(t *testing.T) {
	out := renderSignedIn(t, "user", userFixture())
	pageHas(t, "User", out, `The user's own address. They can change it under Account, and can choose it there as the address that receives DMARC reports.`)
	pageLacks(t, "User", out, `notification`, `reset`)
}

// With domains that follow the user's default report address, deleting them
// leaves those domains with none, and the page says so: up to three names, then
// the count of the rest. With none following, the page says nothing about it.
func TestUserDeleteSaysWhichDomainsLoseTheirReportAddress(t *testing.T) {
	for _, tt := range []struct {
		following []string
		want      string
	}{
		{[]string{"example.org"}, `<strong>example.org</strong> will have no DMARC report address until someone sets one.`},
		{[]string{"example.org", "shop.example.org"}, `<strong>example.org</strong> and <strong>shop.example.org</strong> will have no DMARC report address until someone sets one.`},
		{[]string{"a.example", "b.example", "c.example"}, `<strong>a.example</strong>, <strong>b.example</strong> and <strong>c.example</strong> will have no DMARC report address`},
		{[]string{"a.example", "b.example", "c.example", "d.example", "e.example"}, `<strong>a.example</strong>, <strong>b.example</strong>, <strong>c.example</strong> and 2 more will have no DMARC report address`},
	} {
		in := UserDeleteInput{ID: 3, Username: "acme-ops", AllOutbound: true, Following: tt.following}
		out := renderSignedIn(t, "user-delete", NewUserDelete(admin(), in))
		pageHas(t, "Delete user", out, tt.want)
		if strings.Contains(out, "d.example") && len(tt.following) > 3 {
			t.Errorf("a fourth name is spelled out: %v", tt.following)
		}
	}
	// A global user's domains follow them as well.
	out := renderSignedIn(t, "user-delete", NewUserDelete(admin(), UserDeleteInput{ID: 2, Username: "root", Global: true, Following: []string{"example.org"}}))
	pageHas(t, "Delete user", out, `will have no DMARC report address`)

	out = renderSignedIn(t, "user-delete", userDeleteFixture())
	pageLacks(t, "Delete user", out, `DMARC report address`, `The domains`)
}
