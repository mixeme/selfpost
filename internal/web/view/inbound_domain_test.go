package view

import "testing"

// ---- One inbound domain

func TestInDomainShowsTheMockupsBoxes(t *testing.T) {
	out := renderSignedIn(t, "in-domain", inDomainFixture())
	pageHas(t, "Inbound domain", out,
		`<h1 class="title is-3">lists.example.org</h1>`, `<b>OK</b>`, `<span>MX POINTS HERE</span>`,
		`Accepted on port 25, handed to <span class="sp-mono">mx.internal:25</span> over required TLS · 4 recipients`,
		`action="/inbound/domains/1/dns-recheck"`, `Re-check DNS`,
		`<h2>MX record</h2>`, `<span class="tag is-success is-light">published</span>`, `value="10 mail.example.org."`,
		`<h2>Upstream</h2>`, `action="/inbound/domains/1/upstream"`, `name="host" value="mx.internal"`, `name="port" inputmode="numeric" value="25"`,
		`name="tls_mode"`, `<option value="encrypt" selected>Required</option>`,
		`<h2>Valid recipients</h2>`, `action="/inbound/domains/1/recipients"`, `name="recipient_mode" data-list-mode="list"`,
		`<option value="list" selected>Listed addresses only</option>`, `name="addresses"`,
		"announce@lists.example.org\ndev@lists.example.org\nowner@lists.example.org\nsecurity@lists.example.org",
		`<h2>Spam filter</h2>`, `href="#filter"`, `href="/inbound/domains/1/delete"`, `Delete domain`,
	)
	// What has no source in this build is not drawn.
	pageLacks(t, "Inbound domain", out, `rspamd`, `quarantine`, `Quarantine`, `Log for this domain`, `/inbound/log`, `/inbound/quarantine`, `Allow and deny`)
}

// The Spam filter box states what the server is set up with, and what of it the
// viewer needs: the global role also gets the milter address and the name of the
// variable that turns the filter on.
func TestInDomainSpamFilterBoxInTheFourCases(t *testing.T) {
	domain := func(m Meta, on bool, milter, action string) string {
		p := NewInDomain(m, InDomainInput{ID: 1, Name: "lists.example.org", DNSStatus: "ok", Host: "mx.internal", Port: 25,
			TLSMode: "may", RecipientMode: "any"}).
			WithMX("mail.example.org", DNSCheck{Status: "ok"}).WithFilter(on, milter, action)
		return renderSignedIn(t, "in-domain", p)
	}
	ops := Meta{User: "ops", HasInbound: true}

	out := domain(admin(), true, "inet:antispam:11332", "accept")
	pageHas(t, "filter on, global", out, `<span class="tag is-success is-light">on</span>`,
		`<dt>Applies to</dt><dd>Every inbound domain of this server</dd>`,
		`<dt>Filter</dt><dd class="sp-mono">inet:antispam:11332</dd>`,
		`<dt>If the filter is down</dt><dd>Mail is accepted unfiltered</dd>`)

	out = domain(ops, true, "inet:antispam:11332", "tempfail")
	pageHas(t, "filter on, domain role", out, `<dt>Applies to</dt>`, `Mail is deferred until it answers`)
	pageLacks(t, "filter on, domain role", out, `inet:antispam`, `<dt>Filter</dt>`, `Delete domain`, `Rarely changed`)

	out = domain(admin(), false, "", "")
	pageHas(t, "filter off, global", out, `<span class="tag is-light">off</span>`,
		`Mail for this domain is passed to the upstream unfiltered; turning the filter on is a server setting (<code>INBOUND_ANTISPAM_MILTER</code>).`)
	pageLacks(t, "filter off, global", out, `<dl class="sp-facts">`, `Applies to`)

	out = domain(ops, false, "", "")
	pageHas(t, "filter off, domain role", out, `turning the filter on is a server setting, set by the server&#39;s administrator.`)
	pageLacks(t, "filter off, domain role", out, `INBOUND_ANTISPAM_MILTER`)

	// An action nobody can name is not guessed at.
	out = domain(admin(), true, "unix:/run/spam.sock", "")
	pageHas(t, "filter on, unknown action", out, `unix:/run/spam.sock`)
	pageLacks(t, "filter on, unknown action", out, `If the filter is down`)
}

// The stamp, the record's tag and the menu say the same about the MX check.
func TestInDomainMXStates(t *testing.T) {
	page := func(c DNSCheck) string {
		p := NewInDomain(admin(), InDomainInput{ID: 1, Name: "lists.example.org", DNSStatus: c.Status, Host: "mx.internal", Port: 25, TLSMode: "may"}).
			WithMX("mail.example.org", c).WithFilter(false, "", "")
		return renderSignedIn(t, "in-domain", p)
	}
	out := page(DNSCheck{Status: "error", Detail: "No MX points at mail.example.org (this server).", Found: []string{"10 mx.other.net."}})
	pageHas(t, "mismatch", out, `<b>FAIL</b>`, `NO MX HERE`, `sp-fail`, `<span class="tag is-danger is-light">mismatch</span>`,
		`value="10 mx.other.net."`, `No MX points at mail.example.org (this server).`, `<span class="tag is-danger is-light">fail</span>`)

	out = page(DNSCheck{Status: "error", Detail: "No MX record is published at lists.example.org."})
	pageHas(t, "missing", out, `<span class="tag is-danger is-light">missing</span>`, `No MX record is published at lists.example.org.`)
	pageLacks(t, "missing", out, `In DNS now`)

	out = page(DNSCheck{Status: "unknown"})
	pageHas(t, "unknown", out, `<b>WARN</b>`, `NOT CHECKED`, `not checked</span>`, `<span class="tag is-light">unknown</span>`)
}

// A domain with nothing saved yet says so in its lead; any-recipient mode shows
// an empty list, and what was refused is a flash above the boxes.
func TestInDomainNewAndRefused(t *testing.T) {
	p := NewInDomain(admin(), InDomainInput{ID: 3, Name: "new.example.org", DNSStatus: "unknown", Host: "", Port: 25, TLSMode: "may", RecipientMode: "list"}).
		WithMX("mail.example.org", DNSCheck{Status: "unknown"}).WithFilter(false, "", "").WithResult("", "Host is required.")
	out := renderSignedIn(t, "in-domain", p)
	pageHas(t, "new domain", out, `Not accepting mail yet: no upstream is saved · no recipients listed`,
		`<div class="notification is-danger is-light">`, `Host is required.`, `<span class="tag is-light">0</span>`)

	p = NewInDomain(admin(), InDomainInput{ID: 3, Name: "new.example.org", Host: "h", Port: 25, TLSMode: "none", RecipientMode: "any"}).
		WithMX("mail.example.org", DNSCheck{Status: "ok"}).WithFilter(false, "", "").WithResult("Upstream saved.", "")
	out = renderSignedIn(t, "in-domain", p)
	pageHas(t, "any recipient", out, `without TLS · any recipient`, `<option value="any" selected>Any recipient at this domain</option>`,
		`<option value="none" selected>Off</option>`, `<span class="tag is-light">any</span>`, `<div class="notification is-success is-light">`, `Upstream saved.`)
}

func TestInDomainEscapesWhatPeopleTyped(t *testing.T) {
	p := NewInDomain(admin(), InDomainInput{ID: 1, Name: `<script>alert(1)</script>`, Host: `"><img src=x>`, Port: 25, TLSMode: "may",
		RecipientMode: "list", Addresses: []string{`</textarea><b>x</b>`}}).
		WithMX("mail.example.org", DNSCheck{Status: "error", Found: []string{`<i>x</i>`}}).WithFilter(true, `inet:<u>:1`, "accept").WithResult("", `<s>bad</s>`)
	out := renderSignedIn(t, "in-domain", p)
	pageLacks(t, "Inbound domain", out, `<script>alert(1)`, `<img src=x>`, `</textarea><b>`, `<i>x</i>`, `<u>`, `<s>bad`)
}

// The list's head says whether the instance filters.
func TestInDomainsHeadStatesTheFilter(t *testing.T) {
	out := renderSignedIn(t, "in-domains", inDomainsFixture())
	pageHas(t, "Inbound domains", out, `Spam filter <span class="tag is-success is-light">on</span> · 2 domains`)

	p := NewInDomains(admin(), true).WithFilter(false).WithRows(nil)
	pageHas(t, "Inbound domains", renderSignedIn(t, "in-domains", p), `Spam filter <span class="tag is-light">off</span> · 0 domains`)
	// Either order of the two calls says the same.
	p = NewInDomains(admin(), true).WithRows(nil).WithFilter(false)
	pageHas(t, "Inbound domains", renderSignedIn(t, "in-domains", p), `Spam filter <span class="tag is-light">off</span> · 0 domains`)
}
