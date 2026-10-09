package handlers

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

// The page of one inbound domain is filled from the store, the DNS checker and
// the filter settings; each form's refusal comes back on it as a flash.
func TestInboundDomainPageFromTheStore(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(d.ID)
	if err := h.inbound.SetTransport(d.ID, "mx.internal", "2525", "encrypt"); err != nil {
		t.Fatal(err)
	}
	if err := h.inbound.SetRecipients(d.ID, "list", []string{"a@lists.example.com", "b@lists.example.com"}); err != nil {
		t.Fatal(err)
	}

	body := inboundCall(h, http.MethodGet, "/inbound/domains/"+id, nil, globalPrincipal).Body.String()
	has(t, "domain page", body, `<h1 class="title is-3">lists.example.com</h1>`,
		`handed to <span class="sp-mono">mx.internal:2525</span> over required TLS · 2 recipients`,
		`value="10 mail.example.org."`, `name="host" value="mx.internal"`, `value="2525"`,
		`<option value="encrypt" selected>Required</option>`, "a@lists.example.com\nb@lists.example.com",
		`action="/inbound/domains/`+id+`/upstream"`, `action="/inbound/domains/`+id+`/recipients"`,
		`action="/inbound/domains/`+id+`/dns-recheck"`, `href="/inbound/domains/`+id+`/delete"`)

	rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/upstream", url.Values{
		"host": {""}, "port": {"25"}, "tls_mode": {"may"}}, globalPrincipal)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty host = %d, want 400", rec.Code)
	}
	has(t, "refused upstream", rec.Body.String(), `<div class="notification is-danger is-light">`, `name="host" value="mx.internal"`)

	rec = inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/recipients", url.Values{
		"recipient_mode": {"list"}, "addresses": {"x@other.example"}}, globalPrincipal)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign recipient = %d, want 400", rec.Code)
	}
	has(t, "refused recipients", rec.Body.String(), `<div class="notification is-danger is-light">`, "a@lists.example.com\nb@lists.example.com")

	body = send(h.HandleInboundDetail, &globalPrincipal, http.MethodGet, "/inbound/domains/"+id+"?saved=1", map[string]string{"id": id}, nil).Body.String()
	has(t, "after saving", body, `<div class="notification is-success is-light">`, `Upstream saved.`)
}

// Saving either form untouched, with the values the page sends back, stores
// exactly what was stored.
func TestInboundDomainFormsSaveUntouchedUnchanged(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(d.ID)
	if err := h.inbound.SetTransport(d.ID, "mx.internal", "2525", "none"); err != nil {
		t.Fatal(err)
	}
	if err := h.inbound.SetRecipients(d.ID, "list", []string{"a@lists.example.com", "b@lists.example.com"}); err != nil {
		t.Fatal(err)
	}
	before, err := st.GetInboundDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}

	if rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/upstream", url.Values{
		"host": {before.Host}, "port": {itoa(int64(before.Port))}, "tls_mode": {before.TLSMode}}, globalPrincipal); rec.Code != http.StatusSeeOther {
		t.Fatalf("upstream = %d %s", rec.Code, rec.Body.String())
	}
	if rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/recipients", url.Values{
		"recipient_mode": {before.RecipientMode}, "addresses": {"a@lists.example.com\nb@lists.example.com"}}, globalPrincipal); rec.Code != http.StatusSeeOther {
		t.Fatalf("recipients = %d %s", rec.Code, rec.Body.String())
	}
	after, err := st.GetInboundDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("saving untouched changed the domain:\nbefore %+v\nafter  %+v", before, after)
	}
}

// What the Spam filter box says follows the settings the panel read, and the
// address of the filter is the global role's alone.
func TestInboundDomainSpamFilterBox(t *testing.T) {
	s := newInboundStand(t)
	id := itoa(s.a.ID)
	get := func(ownsA bool) string {
		pr := &s.global
		if ownsA {
			pr = &s.ownsA
		}
		rec := send(s.h.HandleInboundDetail, pr, http.MethodGet, "/inbound/domains/"+id, map[string]string{"id": id}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("detail = %d", rec.Code)
		}
		return rec.Body.String()
	}

	body := get(false)
	has(t, "filter off", body, `turning the filter on is a server setting (<code>INBOUND_ANTISPAM_MILTER</code>)`)
	lacks(t, "filter off", body, `Applies to`)
	has(t, "filter off, domain administrator", get(true), `set by the server&#39;s administrator`)
	lacks(t, "filter off, domain administrator", get(true), `INBOUND_ANTISPAM_MILTER`)

	s.h.cfg.InboundAntispamMilter, s.h.cfg.InboundAntispamAction = "inet:antispam:11332", "tempfail"
	body = get(false)
	has(t, "filter on", body, `Every inbound domain of this server`, `inet:antispam:11332`, `Mail is deferred until it answers`)
	body = get(true)
	has(t, "filter on, domain administrator", body, `Every inbound domain of this server`, `Mail is deferred until it answers`)
	lacks(t, "filter on, domain administrator", body, `inet:antispam`, `/delete`)

	s.h.cfg.InboundAntispamAction = "accept"
	has(t, "filter on, accept", get(false), `Mail is accepted unfiltered`)
	s.h.cfg.InboundAntispamAction = ""
	lacks(t, "filter on, action unknown", get(false), `If the filter is down`)

	// The list says the same in its head.
	list := send(s.h.HandleInboundList, &s.global, http.MethodGet, "/inbound/domains", nil, nil).Body.String()
	has(t, "list", list, `Spam filter <span class="tag is-success is-light">on</span> · 2 domains`)
	s.h.cfg.InboundAntispamMilter = ""
	list = send(s.h.HandleInboundList, &s.global, http.MethodGet, "/inbound/domains", nil, nil).Body.String()
	has(t, "list", list, `Spam filter <span class="tag is-light">off</span> · 2 domains`)
}
