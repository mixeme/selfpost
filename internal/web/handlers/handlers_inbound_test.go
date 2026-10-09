package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

type recordingMaps struct {
	n int
}

func (r *recordingMaps) RebuildInboundMaps(_ []postfix.InboundRoute) error {
	r.n++
	return nil
}

func inboundHandlers(t *testing.T) (*Handlers, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	v := mustView(t)
	v.SetInboundEnabled(true)
	h := &Handlers{
		store:   st,
		inbound: inbound.NewService(st, &recordingMaps{}),
		view:    v,
		cfg:     Config{Version: "test", InboundEnabled: true, Hostname: "mail.example.org"},
	}
	return h, st
}

func inboundCall(h *Handlers, method, target string, form url.Values, p auth.Principal) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req = auth.RequestWithPrincipal(req, p)
	if rest, ok := strings.CutPrefix(req.URL.Path, "/inbound/domains/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		if id != "" && id != "delete" {
			req.SetPathValue("id", id)
		}
	}
	switch {
	case method == http.MethodGet && target == "/inbound/domains":
		h.HandleInboundList(rec, req)
	case method == http.MethodPost && target == "/inbound/domains":
		h.HandleAddInbound(rec, req)
	case strings.HasSuffix(target, "/delete") && method == http.MethodGet:
		h.HandleInboundDeleteConfirm(rec, req)
	case strings.HasSuffix(target, "/delete") && method == http.MethodPost:
		h.HandleInboundDelete(rec, req)
	case strings.HasSuffix(target, "/upstream"):
		h.HandleInboundTransport(rec, req)
	case strings.HasSuffix(target, "/recipients"):
		h.HandleInboundRecipients(rec, req)
	default:
		h.HandleInboundDetail(rec, req)
	}
	return rec
}

func TestInboundListAndAdd(t *testing.T) {
	h, _ := inboundHandlers(t)
	rec := inboundCall(h, http.MethodGet, "/inbound/domains", nil, globalPrincipal)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Add a domain") {
		t.Fatal("list missing add form")
	}

	rec = inboundCall(h, http.MethodPost, "/inbound/domains", url.Values{"name": {"lists.example.com"}}, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add = %d %s", rec.Code, rec.Body.String())
	}

	rec = inboundCall(h, http.MethodGet, "/inbound/domains", nil, globalPrincipal)
	if !strings.Contains(rec.Body.String(), "lists.example.com") {
		t.Fatalf("list missing domain:\n%s", rec.Body.String())
	}
}

func TestInboundDisabledIs404(t *testing.T) {
	h, _ := inboundHandlers(t)
	h.cfg.InboundEnabled = false
	rec := inboundCall(h, http.MethodGet, "/inbound/domains", nil, globalPrincipal)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled inbound = %d, want 404", rec.Code)
	}
}

func TestInboundTransportAndRecipients(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(d.ID)

	rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/upstream", url.Values{
		"host": {"10.0.0.8"}, "port": {"25"}, "tls_mode": {"encrypt"},
	}, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upstream = %d %s", rec.Code, rec.Body.String())
	}

	rec = inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/recipients", url.Values{
		"recipient_mode": {"list"},
		"addresses":      {"staff@lists.example.com\nabuse@other.com"},
	}, globalPrincipal)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign recipient = %d, want 400", rec.Code)
	}

	rec = inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/recipients", url.Values{
		"recipient_mode": {"list"},
		"addresses":      {"staff@lists.example.com"},
	}, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("recipients = %d %s", rec.Code, rec.Body.String())
	}

	rec = inboundCall(h, http.MethodGet, "/inbound/domains/"+id, nil, globalPrincipal)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "10.0.0.8") {
		t.Fatalf("detail =\n%s", rec.Body.String())
	}
}

func TestInboundDelete(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(d.ID)
	rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/delete", nil, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetInboundDomain(d.ID); err == nil {
		t.Fatal("domain still present")
	}
}

// The list and the confirmation are the kit pages: the handlers fill them from
// what is stored, and a refused add comes back on the list with what was typed.
func TestInboundListPageFromTheStore(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.inbound.SetTransport(d.ID, "mx.internal", "2525", "encrypt"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddInboundDomain("bare.example.com"); err != nil {
		t.Fatal(err)
	}

	body := inboundCall(h, http.MethodGet, "/inbound/domains", nil, globalPrincipal).Body.String()
	has(t, "list", body, `<h1 class="title is-3">Inbound domains</h1>`, `<h2>Add a domain</h2>`, `action="/inbound/domains"`,
		`<strong>lists.example.com</strong>`, `>mx.internal:2525<`, `required</span>`, `opportunistic</span>`, `>—<`,
		`0 addresses`, `href="/inbound/domains/`+itoa(d.ID)+`/delete"`, `2 domains`, `>MX</span>`)

	rec := inboundCall(h, http.MethodPost, "/inbound/domains", url.Values{"name": {"lists.example.com"}}, globalPrincipal)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate add = %d, want 409", rec.Code)
	}
	has(t, "refused add", rec.Body.String(), `<div class="notification is-danger is-light">`,
		`That inbound domain is already configured.`, `value="lists.example.com"`)

	rec = inboundCall(h, http.MethodPost, "/inbound/domains", url.Values{"name": {"not a domain"}}, globalPrincipal)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid add = %d, want 400", rec.Code)
	}
	has(t, "invalid add", rec.Body.String(), `notification is-danger`, `value="not a domain"`)

	body = send(h.HandleInboundList, &globalPrincipal, http.MethodGet, "/inbound/domains?deleted=1", nil, nil).Body.String()
	has(t, "after a delete", body, `notification is-success`, `Inbound domain deleted.`)
}

func TestInboundDeletePageNamesWhatGoesWithTheDomain(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(d.ID)

	body := inboundCall(h, http.MethodGet, "/inbound/domains/"+id+"/delete", nil, globalPrincipal).Body.String()
	has(t, "delete page", body, `<h1 class="title is-3">Delete lists.example.com</h1>`, `sp-danger-zone`,
		`stop accepting mail for it on port 25`, `action="/inbound/domains/`+id+`/delete"`,
		`href="/inbound/domains/`+id+`">Keep it</a>`)
	lacks(t, "delete page of a domain with no upstream and no recipients", body, `upstream`, `recipient`)

	if err := h.inbound.SetTransport(d.ID, "mx.internal", "25", "may"); err != nil {
		t.Fatal(err)
	}
	if err := h.inbound.SetRecipients(d.ID, "list", []string{"a@lists.example.com", "b@lists.example.com"}); err != nil {
		t.Fatal(err)
	}
	body = inboundCall(h, http.MethodGet, "/inbound/domains/"+id+"/delete", nil, globalPrincipal).Body.String()
	has(t, "delete page", body, `remove its upstream and its 2 listed recipients.`)

	// Recipients are only a list while the domain accepts listed addresses.
	if err := h.inbound.SetRecipients(d.ID, "any", nil); err != nil {
		t.Fatal(err)
	}
	body = inboundCall(h, http.MethodGet, "/inbound/domains/"+id+"/delete", nil, globalPrincipal).Body.String()
	has(t, "delete page, any recipient", body, `remove its upstream.`)
	lacks(t, "delete page, any recipient", body, `recipient`)
}
