package view

import (
	"strings"
	"testing"
)

// ---- Inbound domains

// The global role adds and deletes domains; a domain administrator sees the
// list alone, so the page has no add box, no Delete link, and numbers the table
// box 01.
func TestInDomainsOffersAddAndDeleteToTheGlobalRoleOnly(t *testing.T) {
	out := renderSignedIn(t, "in-domains", inDomainsFixture())
	pageHas(t, "Inbound domains", out,
		`<h1 class="title is-3">Inbound domains</h1>`, `<h2>Add a domain</h2>`, `action="/inbound/domains"`, `name="name"`,
		`2 domains`, `<th>TLS to upstream</th>`, `href="/inbound/domains/1"><strong>lists.example.org</strong>`,
		`<span class="tag is-success is-light">MX</span>`, `>mx.internal:25<`, `4 addresses`, `any address`, `>10.0.4.12:2525<`,
		`<span class="tag is-success is-light">required</span>`, `<span class="tag is-light">opportunistic</span>`,
		`>Delete</a>`, `href="/inbound/domains/2/delete"`,
		`The MX tag is green when at least one MX of the domain points at this server.`,
	)

	p := NewInDomains(Meta{User: "ops", HasInbound: true}, false).WithRows([]InDomainRow{
		NewInDomainRow(InDomainInput{ID: 1, Name: "lists.example.org", DNSStatus: "ok", Host: "mx.internal", Port: 25,
			TLSMode: "encrypt", RecipientMode: "list", RecipientCount: 1}, false),
	})
	out = renderSignedIn(t, "in-domains", p)
	pageHas(t, "Inbound domains", out, `<span class="sp-no">01</span><h2>Domains</h2>`, `1 domain`, `1 address<`)
	pageLacks(t, "Inbound domains", out, `Add a domain`, `name="name"`, `>Delete</a>`, `/delete`)
}

// The MX tag takes its colour from the check and its word stays MX; an upstream
// not yet saved is a dash and TLS that is off is not dressed up as fine.
func TestInDomainRowsWordTheStoresFields(t *testing.T) {
	p := NewInDomains(admin(), true).WithRows([]InDomainRow{
		NewInDomainRow(InDomainInput{ID: 7, Name: "new.example.org", DNSStatus: "error", TLSMode: "none", RecipientMode: "list"}, true),
		NewInDomainRow(InDomainInput{ID: 8, Name: "warn.example.org", DNSStatus: "warn", Host: "h", Port: 25, TLSMode: "may", RecipientMode: "list", RecipientCount: 2}, true),
		NewInDomainRow(InDomainInput{ID: 9, Name: "unk.example.org", DNSStatus: "unknown", TLSMode: "may", RecipientMode: "any"}, true),
	})
	out := renderSignedIn(t, "in-domains", p)
	pageHas(t, "Inbound domains", out,
		`<span class="tag is-danger is-light">MX</span>`, `<span class="tag is-warning is-light">MX</span>`, `<span class="tag is-light">MX</span>`,
		`>—<`, `0 addresses`, `<span class="tag is-light">off</span>`, `2 addresses`, `3 domains`)
}

func TestInDomainsEmptyAndRefused(t *testing.T) {
	p := NewInDomains(admin(), true).WithRows(nil).WithResult("", "That inbound domain is already configured.", "lists.example.org")
	out := renderSignedIn(t, "in-domains", p)
	pageHas(t, "Inbound domains", out, `No inbound domains yet. Add the first one above`,
		`<div class="notification is-danger is-light">`, `That inbound domain is already configured.`, `value="lists.example.org"`, `0 domains`)
	pageLacks(t, "Inbound domains", out, `<table`)
	// the flash sits between the head and the first box
	if strings.Index(out, `class="notification`) < strings.Index(out, `class="sp-head"`) ||
		strings.Index(out, `class="notification`) > strings.Index(out, `class="box"`) {
		t.Error("the flash is not between page_head and the first box")
	}

	out = renderSignedIn(t, "in-domains", NewInDomains(admin(), true).WithResult("Inbound domain deleted.", "", ""))
	pageHas(t, "Inbound domains", out, `<div class="notification is-success is-light">`, `Inbound domain deleted.`)

	// A domain administrator with nothing listed is not told to add anything.
	out = renderSignedIn(t, "in-domains", NewInDomains(Meta{User: "ops", HasInbound: true}, false))
	pageHas(t, "Inbound domains", out, `No inbound domains yet.`)
	pageLacks(t, "Inbound domains", out, `Add the first one`)
}

func TestInDomainsEscapesWhatPeopleTyped(t *testing.T) {
	p := NewInDomains(admin(), true).WithRows([]InDomainRow{
		NewInDomainRow(InDomainInput{ID: 1, Name: `<script>alert(1)</script>`, Host: `"><img src=x>`, Port: 25}, true),
	}).WithResult("", "bad", `"><i>`)
	out := renderSignedIn(t, "in-domains", p)
	pageLacks(t, "Inbound domains", out, `<script>alert(1)`, `<img src=x>`, `<i>`)
}

// ---- Delete an inbound domain

func TestInDomainDeleteNamesWhatGoesWithIt(t *testing.T) {
	out := renderSignedIn(t, "in-domain-delete", inDomainDeleteFixture())
	pageHas(t, "Delete", out,
		`<h1 class="title is-3">Delete lists.example.org</h1>`, `<div class="box sp-danger-zone">`, `<h2>This cannot be undone</h2>`,
		`Deleting the inbound domain <strong>lists.example.org</strong> will:`,
		`stop accepting mail for it on port 25 — senders get a relay-denied reply;`,
		`remove its upstream and its 4 listed recipients.`, `Outbound domains are not touched.`,
		`<form method="post" action="/inbound/domains/1/delete">`, `class="button is-danger"`, `Delete lists.example.org</button>`,
		`href="/inbound/domains/1">Keep it</a>`,
	)

	// Only what the domain really has is named.
	out = renderSignedIn(t, "in-domain-delete", NewInDomainDelete(admin(), 1, "lists.example.org", true, 0))
	pageHas(t, "Delete", out, `remove its upstream.`)
	pageLacks(t, "Delete", out, `recipient`)
	out = renderSignedIn(t, "in-domain-delete", NewInDomainDelete(admin(), 1, "lists.example.org", false, 1))
	pageHas(t, "Delete", out, `remove its 1 listed recipient.`)
	pageLacks(t, "Delete", out, `upstream`)
	out = renderSignedIn(t, "in-domain-delete", NewInDomainDelete(admin(), 1, "lists.example.org", false, 0))
	pageHas(t, "Delete", out, `stop accepting mail for it`)
	pageLacks(t, "Delete", out, `upstream`, `recipient`)
}
