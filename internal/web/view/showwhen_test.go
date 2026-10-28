package view

import (
	"regexp"
	"testing"
)

// A block panel.js shows or hides (its show-when rules) arrives in the state
// the script would give it: a block drawn open and closed by the script after
// the first paint made the page jump on every load - the Settings of a domain
// most, measured on 2026-10-28.
func TestShowWhenBlocksArriveInTheirState(t *testing.T) {
	block := func(t *testing.T, out, attr string) string {
		t.Helper()
		m := regexp.MustCompile(`<div[^>]*`+attr+`[^>]*>`).FindAllString(out, -1)
		if len(m) == 0 {
			t.Fatalf("no block with %s", attr)
		}
		return m[0]
	}
	hidden := func(tag string) bool {
		return regexp.MustCompile(`class="[^"]*\bis-hidden\b`).MatchString(tag) && regexp.MustCompile(` hidden[ >]`).MatchString(tag)
	}
	for _, c := range []struct {
		name, page, attr string
		data             func() any
		want             bool
	}{
		{"backup, not encrypted", "backup", "data-encrypt-fields", func() any { b := backupFixture(); b.Encrypt = false; return b }, true},
		{"backup, encrypted", "backup", "data-encrypt-fields", func() any { return backupFixture() }, false},
		{"import password before a file is chosen", "backup", "data-import-password-fields", func() any { return backupFixture() }, true},
		{"domain export, not encrypted", "out-domain-settings", "data-encrypt-fields", func() any { s := outDomainSettingsFixture(); s.Encrypt = false; return s }, true},
		{"report address, hosted", "out-domain-settings", "data-custom-address", func() any { s := outDomainSettingsFixture(); s.RuaSelected = ReportHosted; return s }, true},
		{"report address, typed", "out-domain-settings", "data-custom-address", func() any { s := outDomainSettingsFixture(); s.RuaSelected = ReportCustom; return s }, false},
		{"domain limit, auto: manual fields", "out-domain-settings", "data-manual-fields", func() any { s := outDomainSettingsFixture(); s.Rate.Auto = true; return s }, true},
		{"domain limit, auto: auto fields", "out-domain-settings", "data-auto-fields", func() any { s := outDomainSettingsFixture(); s.Rate.Auto = true; return s }, false},
		{"domain limit, manual: auto fields", "out-domain-settings", "data-auto-fields", func() any { s := outDomainSettingsFixture(); s.Rate.Auto = false; return s }, true},
		{"application, any address", "out-app", "data-addresses", func() any { a := outAppFixture(); a.Mode = "wildcard"; return a }, true},
		{"application, a list", "out-app", "data-addresses", func() any { a := outAppFixture(); a.Mode = "list"; return a }, false},
		{"application limit, the domain's", "out-app", "data-manual-fields", func() any { a := outAppFixture(); a.LimitMode = "domain"; return a }, true},
		{"inbound recipients, any", "in-domain", "data-addresses", func() any { d := inDomainFixture(); d.ModeSelected = "any"; return d }, true},
		{"inbound recipients, a list", "in-domain", "data-addresses", func() any { d := inDomainFixture(); d.ModeSelected = "list"; return d }, false},
		{"user, global", "user", "data-domain-pick", func() any { u := userFixture(); u.Outbound.Hidden, u.Inbound.Hidden = true, true; return u }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			tag := block(t, renderSignedIn(t, c.page, c.data()), c.attr)
			if hidden(tag) != c.want {
				t.Errorf("%s: hidden = %v, want %v", tag, hidden(tag), c.want)
			}
		})
	}
}

// The user form hides the lists of domains while the role is Global, from the
// role the form is drawn with (NewUserForm).
func TestUserFormHidesTheDomainsOfAGlobalUser(t *testing.T) {
	in := UserFormInput{Role: "global"}
	p := NewUserForm(Meta{User: "admin", IsGlobal: true}, in)
	if !p.Outbound.Hidden || !p.Inbound.Hidden {
		t.Error("a global user's form shows the lists of domains")
	}
	in.Role = "domain"
	if p = NewUserForm(Meta{User: "admin", IsGlobal: true}, in); p.Outbound.Hidden || p.Inbound.Hidden {
		t.Error("a domain user's form hides the lists of domains")
	}
}
