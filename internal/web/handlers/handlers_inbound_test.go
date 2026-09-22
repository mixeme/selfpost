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
	if rest, ok := strings.CutPrefix(req.URL.Path, "/inbound/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		if id != "" && id != "delete" {
			req.SetPathValue("id", id)
		}
	}
	switch {
	case method == http.MethodGet && target == "/inbound":
		h.HandleInboundList(rec, req)
	case method == http.MethodPost && target == "/inbound":
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
	rec := inboundCall(h, http.MethodGet, "/inbound", nil, globalPrincipal)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Add inbound domain") {
		t.Fatal("list missing add form")
	}

	rec = inboundCall(h, http.MethodPost, "/inbound", url.Values{"name": {"lists.example.com"}}, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add = %d %s", rec.Code, rec.Body.String())
	}

	rec = inboundCall(h, http.MethodGet, "/inbound", nil, globalPrincipal)
	if !strings.Contains(rec.Body.String(), "lists.example.com") {
		t.Fatalf("list missing domain:\n%s", rec.Body.String())
	}
}

func TestInboundDisabledIs404(t *testing.T) {
	h, _ := inboundHandlers(t)
	h.cfg.InboundEnabled = false
	rec := inboundCall(h, http.MethodGet, "/inbound", nil, globalPrincipal)
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

	rec := inboundCall(h, http.MethodPost, "/inbound/"+id+"/upstream", url.Values{
		"host": {"10.0.0.8"}, "port": {"25"}, "tls_mode": {"encrypt"},
	}, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upstream = %d %s", rec.Code, rec.Body.String())
	}

	rec = inboundCall(h, http.MethodPost, "/inbound/"+id+"/recipients", url.Values{
		"recipient_mode": {"list"},
		"addresses":      {"staff@lists.example.com\nabuse@other.com"},
	}, globalPrincipal)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign recipient = %d, want 400", rec.Code)
	}

	rec = inboundCall(h, http.MethodPost, "/inbound/"+id+"/recipients", url.Values{
		"recipient_mode": {"list"},
		"addresses":      {"staff@lists.example.com"},
	}, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("recipients = %d %s", rec.Code, rec.Body.String())
	}

	rec = inboundCall(h, http.MethodGet, "/inbound/"+id, nil, globalPrincipal)
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
	rec := inboundCall(h, http.MethodPost, "/inbound/"+id+"/delete", nil, globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetInboundDomain(d.ID); err == nil {
		t.Fatal("domain still present")
	}
}
