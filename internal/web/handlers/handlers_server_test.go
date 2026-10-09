package handlers

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// ---- System log

// The page and the fragment polled from it show the tail of mail.log, newest
// line first, with the time split off each line.
func TestSystemLogPageAndFragmentShowTheTail(t *testing.T) {
	h := &Handlers{view: mustView(t), cfg: Config{Version: "test"}}
	h.cfg.MailLogPath = writeMailLog(t,
		"Sep 21 13:58:09 mail postfix/smtp[2123]: 4XcB7k2Jm9z1: to=<noc@example.org>, status=deferred (451 Greylisted)",
		"Sep 21 14:01:52 mail postfix/qmgr[58]: 4XcB9q0Lw2z1: removed",
		"a line the log format does not stamp",
	)

	page := getBody(t, h.HandleSystemLog, "/server/log")
	for _, want := range []string{
		`<h1 class="title is-3">System log</h1>`, `hx-get="/server/log/fragment"`,
		`<span class="sp-d">Sep 21 14:01:52</span> mail postfix/qmgr[58]: 4XcB9q0Lw2z1: removed`,
		`<span class="sp-w">mail postfix/smtp[2123]`, // a deferral is amber
		`a line the log format does not stamp`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the system log page is missing %q", want)
		}
	}
	if strings.Index(page, "a line the log format") > strings.Index(page, "postfix/qmgr") ||
		strings.Index(page, "postfix/qmgr") > strings.Index(page, "postfix/smtp[2123]") {
		t.Error("the log is not shown newest line first")
	}

	frag := getBody(t, h.HandleSystemLogBody, "/server/log/fragment")
	if !strings.HasPrefix(frag, `<div class="box" id="system-log" data-poll`) || strings.Contains(frag, "<html") {
		t.Errorf("the fragment is not the polled box alone:\n%.300s", frag)
	}
	if !strings.Contains(frag, "postfix/qmgr[58]") {
		t.Error("the fragment does not carry the log")
	}
}

// A log that is not there yet (rotation is renaming it) is the ordinary empty
// state, not a fault. (The page of a log that cannot be read is in the view's
// tests: a read error cannot be made the same way on every platform.)
func TestSystemLogMissingIsTheEmptyState(t *testing.T) {
	h := &Handlers{view: mustView(t), cfg: Config{Version: "test"}}
	h.cfg.MailLogPath = t.TempDir() + "/never-created.log"
	if out := getBody(t, h.HandleSystemLog, "/server/log"); !strings.Contains(out, "No log lines yet.") || strings.Contains(out, "Could not read") {
		t.Errorf("a missing log should be the empty state:\n%s", out)
	}
}

// ---- Users

func TestUsersPageListsEveryoneWithTheirReach(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	in1, _ := st.AddInboundDomain("in.example.com")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	userWith(t, st, "shop", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}, InboundDomainIDs: []int64{in1.ID}})
	userWith(t, st, "everything", store.RoleDomain, store.Reach{AllDomains: true})
	if err := st.UpdateUser(root.ID, "root", "test-hash", "root@example.org"); err != nil {
		t.Fatal(err)
	}

	body := send(h.HandleUsers, &root, "GET", "/server/users?done=updated", nil, nil).Body.String()
	for _, want := range []string{
		`<strong>root</strong> <span class="tag is-light">you</span>`, `root@<wbr>example.org`,
		`<span class="tag is-primary is-light">global</span>`, `<span class="tag is-light">domain</span>`,
		`<td class="sp-muted">out.example.com</td><td class="sp-muted">in.example.com</td>`,
		`<td class="sp-muted">All</td><td class="sp-muted">—</td>`,
		`User updated.`, `3 users`, `href="/server/users/new"`, `<th>Inbound domains</th>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the users page is missing %q", want)
		}
	}

	h.cfg.InboundEnabled = false
	if body := send(h.HandleUsers, &root, "GET", "/server/users", nil, nil).Body.String(); strings.Contains(body, "<th>Inbound domains</th>") {
		t.Error("the users list has an inbound column while the inbound feature is off")
	}
}

// ---- The user form

var (
	tagRe    = regexp.MustCompile(`<(input|select)\b[^>]*>`)
	attrRe   = regexp.MustCompile(`([a-z-]+)(?:="([^"]*)")?`)
	optionRe = regexp.MustCompile(`<option value="([^"]*)"( selected)?>`)
)

// submitted returns what a browser would send when the user form in body is
// saved untouched: every control that is not disabled, a checkbox only when it
// is checked, a select as its selected option, a text field as it is filled.
func submitted(t *testing.T, body string) url.Values {
	t.Helper()
	start := strings.Index(body, `<form class="sp-form"`)
	end := strings.Index(body[start:], "</form>")
	if start < 0 || end < 0 {
		t.Fatalf("no user form in the page:\n%s", body)
	}
	form := body[start : start+end]
	values := url.Values{}
	for _, loc := range tagRe.FindAllStringIndex(form, -1) {
		tag := form[loc[0]:loc[1]]
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(strings.TrimSuffix(strings.TrimPrefix(tag, "<"), ">"), -1) {
			attrs[m[1]] = html.UnescapeString(m[2])
		}
		if _, off := attrs["disabled"]; off || attrs["name"] == "" {
			continue
		}
		switch {
		case strings.HasPrefix(tag, "<select"):
			closeAt := strings.Index(form[loc[1]:], "</select>")
			for _, o := range optionRe.FindAllStringSubmatch(form[loc[1]:loc[1]+closeAt], -1) {
				if o[2] != "" {
					values.Add(attrs["name"], html.UnescapeString(o[1]))
				}
			}
		case attrs["type"] == "checkbox":
			if _, on := attrs["checked"]; on {
				values.Add(attrs["name"], attrs["value"])
			}
		default:
			values.Add(attrs["name"], attrs["value"])
		}
	}
	return values
}

// Saving the user form untouched must not alter the user: the role, both lists,
// both All flags and the account e-mail come back as they were, the password
// field is blank and so keeps the hash.
func TestUserFormSavedUntouchedChangesNothing(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("one.example.com", "mail")
	out2, _ := st.AddDomain("two.example.com", "mail")
	in1, _ := st.AddInboundDomain("in.example.com")
	in2, _ := st.AddInboundDomain("other.example.com")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})

	cases := map[string]store.Reach{
		"named":    {DomainIDs: []int64{out1.ID}, InboundDomainIDs: []int64{in2.ID}},
		"all-out":  {AllDomains: true, InboundDomainIDs: []int64{in1.ID, in2.ID}},
		"all-in":   {DomainIDs: []int64{out1.ID, out2.ID}, AllInbound: true},
		"all-both": {AllDomains: true, AllInbound: true},
	}
	for name, reach := range cases {
		t.Run(name, func(t *testing.T) {
			p := userWith(t, st, "user-"+name, store.RoleDomain, reach)
			email := name + "@example.org"
			if name == "all-both" {
				email = "" // a user who has none must come back with none
			}
			if err := st.UpdateUser(p.ID, "user-"+name, "a-real-hash", email); err != nil {
				t.Fatal(err)
			}
			before, _ := st.GetUser(p.ID)
			path := "/server/users/" + idStr(p.ID)
			pv := map[string]string{"uid": idStr(p.ID)}

			get := send(h.HandleUserEdit, &root, "GET", path, pv, nil)
			if get.Code != http.StatusOK {
				t.Fatalf("GET = %d", get.Code)
			}
			form := submitted(t, get.Body.String())
			if form.Get("password") != "" {
				t.Fatalf("the password field is not blank: %q", form.Get("password"))
			}
			if _, sent := form["email"]; !sent || form.Get("email") != email {
				t.Fatalf("the form sends e-mail %q (sent: %v), want %q", form.Get("email"), sent, email)
			}
			post := send(h.HandleUserEdit, &root, "POST", path, pv, form)
			if post.Code != http.StatusSeeOther {
				t.Fatalf("POST of the untouched form = %d:\n%s", post.Code, post.Body.String())
			}
			after, _ := st.GetUser(p.ID)
			if after.Username != before.Username || after.PasswordHash != before.PasswordHash || after.Role != before.Role ||
				after.Email != before.Email || after.DMARCDefaultMode != before.DMARCDefaultMode ||
				!equalReach(after.Reach(), before.Reach()) {
				t.Errorf("saving untouched changed the user:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
}

func equalReach(a, b store.Reach) bool {
	same := func(x, y []int64) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return a.AllDomains == b.AllDomains && a.AllInbound == b.AllInbound &&
		same(a.DomainIDs, b.DomainIDs) && same(a.InboundDomainIDs, b.InboundDomainIDs)
}

// A global user is saved untouched too, and the only global user — whose role
// select is read-only — keeps the role through the hidden field.
func TestUserFormOfTheOnlyGlobalUserSavesUntouched(t *testing.T) {
	h, st := inboundHandlers(t)
	st.AddDomain("one.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	before, _ := st.GetUser(root.ID)
	path := "/server/users/" + idStr(root.ID)
	pv := map[string]string{"uid": idStr(root.ID)}

	get := send(h.HandleUserEdit, &root, "GET", path, pv, nil).Body.String()
	for _, want := range []string{`<input type="hidden" name="role" value="global">`, `<select id="role" name="role" data-global-role="global" disabled>`} {
		if !strings.Contains(get, want) {
			t.Errorf("the form of the only global user is missing %q", want)
		}
	}
	if strings.Contains(get, "/delete") {
		t.Error("the only global user is offered Delete user")
	}
	form := submitted(t, get)
	if form.Get("role") != "global" {
		t.Fatalf("the untouched form sends role %q", form.Get("role"))
	}
	if rec := send(h.HandleUserEdit, &root, "POST", path, pv, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ := st.GetUser(root.ID)
	if after.Role != before.Role || after.PasswordHash != before.PasswordHash || !after.Reach().Empty() {
		t.Errorf("saving the only global user untouched changed them: %+v -> %+v", before, after)
	}

	// Another global user exists: the role is a real choice again, and Delete is offered.
	other := userWith(t, st, "second", store.RoleGlobal, store.Reach{})
	get = send(h.HandleUserEdit, &root, "GET", "/server/users/"+idStr(other.ID), map[string]string{"uid": idStr(other.ID)}, nil).Body.String()
	if strings.Contains(get, "disabled") || !strings.Contains(get, `href="/server/users/`+idStr(other.ID)+`/delete"`) {
		t.Errorf("with two global users the form should be free:\n%s", get)
	}
}

// A refused save comes back as the flash with the form as it was typed, the head
// naming the stored user.
func TestUserFormRefusalIsTheFlash(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	userWith(t, st, "taken", store.RoleGlobal, store.Reach{})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(root.ID), map[string]string{"uid": idStr(root.ID)},
		url.Values{"username": {"taken"}, "role": {"global"}})
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("a taken name = %d", rec.Code)
	}
	for _, want := range []string{`<div class="notification is-danger is-light">`, "That username is already in use.",
		`<h1 class="title is-3">root</h1>`, `name="username" value="taken"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused form is missing %q", want)
		}
	}

	// The create form comes back the same way, with what was typed.
	rec = send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, url.Values{"username": {"fresh"}, "password": {"short"}, "role": {"domain"}})
	body = rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "password must be at least") ||
		!strings.Contains(body, `<h1 class="title is-3">Create user</h1>`) || !strings.Contains(body, `name="username" value="fresh"`) {
		t.Errorf("a refused create form = %d:\n%s", rec.Code, body)
	}
}

// ---- Delete a user

func TestUserDeletePageNamesTheAssignmentsAndRefusalsStayOnTheForm(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	in1, _ := st.AddInboundDomain("in.example.com")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "shop", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}, InboundDomainIDs: []int64{in1.ID}})

	path := "/server/users/" + idStr(u.ID) + "/delete"
	body := send(h.HandleUserDeleteConfirm, &root, "GET", path, map[string]string{"uid": idStr(u.ID)}, nil).Body.String()
	for _, want := range []string{
		`<h1 class="title is-3">Delete shop</h1>`, `<div class="box sp-danger-zone">`,
		`remove the assignments to <strong>out.example.com</strong> (outbound) and to <strong>in.example.com</strong> (inbound).`,
		`<form method="post" action="` + path + `">`, `href="/server/users/` + idStr(u.ID) + `">Keep it`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the delete page is missing %q", want)
		}
	}

	// Nobody deletes themselves or the last global user: the refusal is the flash on the form.
	rec := send(h.HandleUserDelete, &root, "POST", "/server/users/"+idStr(root.ID)+"/delete", map[string]string{"uid": idStr(root.ID)}, url.Values{})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "You cannot delete your own account while signed in.") ||
		!strings.Contains(rec.Body.String(), `class="notification is-danger is-light"`) {
		t.Errorf("deleting yourself = %d:\n%s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetUser(root.ID); err != nil {
		t.Errorf("the refused delete removed the user: %v", err)
	}
	if rec := send(h.HandleUserDelete, &root, "POST", path, map[string]string{"uid": idStr(u.ID)}, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("deleting another user = %d", rec.Code)
	}
	if _, err := st.GetUser(u.ID); err == nil {
		t.Error("the user is still there after the confirmed delete")
	}
}

// ---- Help

// Help is for both roles. The topics about the checks of Overview and Health are
// shown to the global role only.
func TestHelpPageForBothRoles(t *testing.T) {
	h, st := inboundHandlers(t)
	dom, _ := st.AddDomain("out.example.com", "mail")
	ops := userWith(t, st, "ops", store.RoleDomain, store.Reach{DomainIDs: []int64{dom.ID}})
	root := auth.Principal{ID: 1, Username: "admin", Role: auth.RoleGlobal}

	for _, tt := range []struct {
		who       auth.Principal
		wantCheck bool
	}{{root, true}, {ops, false}} {
		rec := send(h.HandleHelp, &tt.who, "GET", "/help", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /help as %s = %d", tt.who.Username, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{`<h1 class="title is-3">Help</h1>`, `id="dns"`, `id="apps"`, `id="limits"`} {
			if !strings.Contains(body, want) {
				t.Errorf("Help as %s is missing %q", tt.who.Username, want)
			}
		}
		if got := strings.Contains(body, `id="checks"`); got != tt.wantCheck {
			t.Errorf("Help as %s: Overview topics present = %v, want %v", tt.who.Username, got, tt.wantCheck)
		}
	}
}

// ---- Backup

// A refused pre-flight says what to add to docker-compose.yml as plain text: the
// message is escaped on the page, so a tag in it would be printed as one.
func TestBackupDeployRefusalIsPlainText(t *testing.T) {
	msg := deployBackupErr(errString("DeployRoot is not mounted"))
	if strings.ContainsAny(msg, "<>") || !strings.Contains(msg, ".:/selfpost-deploy:ro") {
		t.Errorf("deployBackupErr = %q", msg)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
