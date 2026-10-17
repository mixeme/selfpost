package e2e

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testPasswordReset (password-reset): the whole way, through the sink, as
// the administrator would. The address saved for notifications was sent its
// confirmation link, and the restore of an earlier step deleted it (a
// restore deletes every link), so Account sends it again; opening it changes
// nothing until its button is pressed. Then, signed out, Forgot your
// password? sends a link to that confirmed address; it sets a new password,
// which signs in, and ends every session - the scenario's own included,
// which signs in again with the new one.
func testPasswordReset(t *testing.T, sc *scenario) {
	// links are the links of path in the sink's messages to the
	// administrator.
	links := func(path string) map[string]bool {
		t.Helper()
		re := regexp.MustCompile(`https://` + regexp.QuoteMeta(selfpostHostname) + `(` + regexp.QuoteMeta(path) + `[A-Za-z0-9_-]{43})\r?\n`)
		dir := filepath.Join(h.stageDir, "mail-stage")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil || !strings.Contains(string(b), "To: <"+adminEmail+">") {
				continue
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				out[m[1]] = true
			}
		}
		return out
	}
	// newLink waits for a link of path that before did not have.
	newLink := func(path string, before map[string]bool) string {
		t.Helper()
		var link string
		if err := waitFor("a new link to "+path+" in the sink", 60*time.Second, 500*time.Millisecond, func() (bool, error) {
			for l := range links(path) {
				if !before[l] {
					link = l
					return true, nil
				}
			}
			return false, nil
		}); err != nil {
			t.Fatalf("%v\n%s", err, tailLines(h.logs("selfpost"), "link", 20))
		}
		return link
	}

	before := links("/confirm/")
	if len(before) == 0 {
		t.Fatal("saving the administrator's address sent no confirmation link")
	}
	if resp, body, err := sc.panel.postForm("/account/email/confirm", nil); err != nil || resp.Request.URL.Path != "/account" || !strings.Contains(body, "A new link is on its way.") {
		t.Fatalf("send the link again: %v\n%.2000s", err, body)
	}
	confirm := newLink("/confirm/", before)
	// The link sent before the restore no longer works.
	for old := range before {
		if _, page, _ := sc.panel.get(old); !strings.Contains(page, "This link has expired or was already used.") {
			t.Errorf("a link sent before the restore still works")
		}
	}
	_, page, err := sc.panel.get(confirm)
	if err != nil || !strings.Contains(page, `action="`+confirm+`"`) {
		t.Fatalf("the confirmation link's page: %v\n%.2000s", err, page)
	}
	if _, account, _ := sc.panel.get("/account"); !strings.Contains(account, "not confirmed") {
		t.Fatalf("opening the link confirmed the address:\n%.3000s", account)
	}
	if _, done, err := sc.panel.postForm(confirm, nil); err != nil || !strings.Contains(done, "E-mail confirmed") {
		t.Fatalf("confirm: %v\n%.2000s", err, done)
	}
	if _, account, _ := sc.panel.get("/account"); !strings.Contains(account, `<span class="tag is-success is-light">confirmed</span>`) {
		t.Fatalf("the address is not confirmed on Account:\n%.3000s", account)
	}

	stranger, err := newPanelClient()
	if err != nil {
		t.Fatal(err)
	}
	resp, body, err := stranger.postForm("/login/reset", url.Values{"username": {adminUser}})
	if err != nil || resp.Request.URL.Path != "/login" || !strings.Contains(body, "the link is on its way") {
		t.Fatalf("ask for a reset link: %v, landed on %s\n%.2000s", err, resp.Request.URL.Path, body)
	}
	reset := newLink("/login/reset/", nil)
	const fresh = "a-brand-new-e2e-password"
	resp, body, err = stranger.postForm(reset, url.Values{"new_password": {fresh}, "new_password_confirm": {fresh}})
	if err != nil || resp.Request.URL.Path != "/login" || !strings.Contains(body, "Password set. Sign in with it.") {
		t.Fatalf("set the new password: %v, landed on %s\n%.2000s", err, resp.Request.URL.Path, body)
	}
	if resp, _, _ := stranger.postForm(reset, url.Values{"new_password": {fresh + "x"}, "new_password_confirm": {fresh + "x"}}); resp == nil || resp.StatusCode != 404 {
		t.Errorf("the reset link worked twice")
	}
	if err := stranger.login(adminUser, adminPassword); err == nil {
		t.Error("the old password still signs in")
	}
	// The scenario's session ended with the reset.
	if resp, err := sc.panel.status(); err != nil || resp.Request.URL.Path != "/login" {
		t.Fatalf("the scenario's session survived the reset")
	}
	if err := sc.panel.login(adminUser, fresh); err != nil {
		t.Fatalf("sign in with the new password: %v", err)
	}
	if err := stranger.login(adminUser, fresh); err != nil {
		t.Fatalf("sign in with the new password elsewhere: %v", err)
	}
}
