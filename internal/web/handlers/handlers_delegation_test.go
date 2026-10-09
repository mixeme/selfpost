package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// userWith creates a panel user with the given role and reach and returns the
// principal the auth middleware would attach for them — read back from the
// store, so the test exercises the same mapping a real request gets.
func userWith(t *testing.T, st *store.Store, username string, role store.Role, reach store.Reach) auth.Principal {
	t.Helper()
	id, err := st.CreateUser(username, "test-hash", role, reach)
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	u, err := st.GetUser(id)
	if err != nil {
		t.Fatalf("get user %s: %v", username, err)
	}
	return auth.Principal{
		ID: u.ID, Username: u.Username, Role: u.Role,
		Domains: u.DomainIDs, AllDomains: u.AllDomains,
		InboundDomains: u.InboundDomainIDs, AllInbound: u.AllInboundDomains,
	}
}

func idStr(id int64) string { return strconv.FormatInt(id, 10) }

// send runs one request against a handler as p (nil: no principal at all).
func send(h http.HandlerFunc, p *auth.Principal, method, target string, pathValues map[string]string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	if p != nil {
		req = auth.RequestWithPrincipal(req, *p)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// inboundRoute is one inbound handler taking an {id}.
type inboundRoute struct {
	method, suffix string
	handler        func(*Handlers) http.HandlerFunc
	form           url.Values
}

// The pages and actions of ONE inbound domain: open to the global role and to a
// domain administrator this inbound domain is assigned to.
var inboundDomainRoutes = []inboundRoute{
	{"GET", "", func(h *Handlers) http.HandlerFunc { return h.HandleInboundDetail }, nil},
	{"POST", "/upstream", func(h *Handlers) http.HandlerFunc { return h.HandleInboundTransport },
		url.Values{"host": {"mx.internal.example"}, "port": {"25"}, "tls_mode": {"may"}}},
	{"POST", "/recipients", func(h *Handlers) http.HandlerFunc { return h.HandleInboundRecipients },
		url.Values{"recipient_mode": {"any"}}},
	{"POST", "/dns-recheck", func(h *Handlers) http.HandlerFunc { return h.HandleInboundDNSRecheck }, nil},
}

// Adding and deleting an inbound domain: the global role only, whatever a
// domain administrator is assigned.
var inboundGlobalRoutes = []inboundRoute{
	{"GET", "/delete", func(h *Handlers) http.HandlerFunc { return h.HandleInboundDeleteConfirm }, nil},
	{"POST", "/delete", func(h *Handlers) http.HandlerFunc { return h.HandleInboundDelete }, nil},
}

type inboundStand struct {
	h                        *Handlers
	a, b                     store.InboundDomain
	global, ownsA, allIn     auth.Principal
	outboundOnly, sameNameAs auth.Principal
}

// newInboundStand is a panel with two inbound domains and one user of each kind
// the delegation has to tell apart.
func newInboundStand(t *testing.T) inboundStand {
	t.Helper()
	h, st := inboundHandlers(t)
	s := inboundStand{h: h}
	var err error
	if s.a, err = st.AddInboundDomain("a.example.com"); err != nil {
		t.Fatal(err)
	}
	if s.b, err = st.AddInboundDomain("b.example.com"); err != nil {
		t.Fatal(err)
	}
	// A SENDING domain with the same name as inbound domain B, at an id that
	// collides with inbound A's: outbound and inbound are separate entities,
	// and neither the name nor the number may leak access across.
	out, err := st.AddDomain("b.example.com", "mail")
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != s.a.ID {
		t.Fatalf("the stand expects the outbound domain's id (%d) to collide with inbound A's (%d)", out.ID, s.a.ID)
	}
	s.global = userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	s.ownsA = userWith(t, st, "owns-a", store.RoleDomain, store.Reach{InboundDomainIDs: []int64{s.a.ID}})
	s.allIn = userWith(t, st, "all-inbound", store.RoleDomain, store.Reach{AllInbound: true})
	s.outboundOnly = userWith(t, st, "outbound-only", store.RoleDomain, store.Reach{AllDomains: true})
	s.sameNameAs = userWith(t, st, "same-name", store.RoleDomain, store.Reach{DomainIDs: []int64{out.ID}})
	return s
}

func (s inboundStand) call(rt inboundRoute, p *auth.Principal, id int64) *httptest.ResponseRecorder {
	return send(rt.handler(s.h), p, rt.method, "/inbound/domains/"+idStr(id)+rt.suffix, map[string]string{"id": idStr(id)}, rt.form)
}

// Every route of one inbound domain, for every kind of user: 404 for another
// tenant's domain, for a domain that does not exist, for a user with no inbound
// reach, for an outbound assignment of the same id or name, and with no
// principal — and the same answer in each case, so nothing tells them apart.
func TestInboundDomainRoutesAreDelegatedPerDomain(t *testing.T) {
	s := newInboundStand(t)
	const missing = 9999
	for _, rt := range inboundDomainRoutes {
		name := rt.method + " /inbound/domains/{id}" + rt.suffix
		denied := []struct {
			who string
			p   *auth.Principal
			id  int64
		}{
			{"a domain administrator of inbound A, on B", &s.ownsA, s.b.ID},
			{"a domain administrator of inbound A, on a missing id", &s.ownsA, missing},
			{"a domain administrator with outbound reach only", &s.outboundOnly, s.a.ID},
			{"a domain administrator of the outbound domain with the same id", &s.sameNameAs, s.a.ID},
			{"a domain administrator of the outbound domain with the same name", &s.sameNameAs, s.b.ID},
			{"no principal", nil, s.a.ID},
		}
		var bodies []string
		for _, c := range denied {
			rec := s.call(rt, c.p, c.id)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s as %s = %d, want 404:\n%s", name, c.who, rec.Code, rec.Body.String())
			}
			bodies = append(bodies, rec.Body.String())
		}
		for _, b := range bodies[1:] {
			if b != bodies[0] {
				t.Errorf("%s: the 404 for another tenant's domain differs from the 404 for a missing one — the panel confirms what exists", name)
				break
			}
		}

		// The DNS re-check resolves real names, so its allowed side is not
		// run here; its guard is the same call as the three above.
		if rt.suffix == "/dns-recheck" {
			continue
		}
		for _, c := range []struct {
			who string
			p   *auth.Principal
			id  int64
		}{
			{"the global role", &s.global, s.b.ID},
			{"its domain administrator", &s.ownsA, s.a.ID},
			{"a domain administrator with All inbound", &s.allIn, s.b.ID},
		} {
			rec := s.call(rt, c.p, c.id)
			if rec.Code != http.StatusOK && rec.Code != http.StatusSeeOther {
				t.Errorf("%s as %s = %d, want it to go through:\n%s", name, c.who, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestAddingAndDeletingInboundDomainsStaysGlobal(t *testing.T) {
	s := newInboundStand(t)
	for _, p := range []*auth.Principal{&s.ownsA, &s.allIn, &s.outboundOnly, nil} {
		for _, rt := range inboundGlobalRoutes {
			if rec := s.call(rt, p, s.a.ID); rec.Code != http.StatusNotFound {
				t.Errorf("%s /inbound/domains/{id}%s as %+v = %d, want 404", rt.method, rt.suffix, p, rec.Code)
			}
		}
		rec := send(s.h.HandleAddInbound, p, "POST", "/inbound/domains", nil, url.Values{"name": {"new.example.com"}})
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST /inbound/domains as %+v = %d, want 404", p, rec.Code)
		}
	}
	list, err := s.h.store.ListInboundDomains()
	if err != nil || len(list) != 2 {
		t.Fatalf("after the refused requests there are %d inbound domains (%v), want the original 2", len(list), err)
	}

	// The global role still can.
	if rec := send(s.h.HandleAddInbound, &s.global, "POST", "/inbound/domains", nil, url.Values{"name": {"new.example.com"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("POST /inbound/domains as the global role = %d:\n%s", rec.Code, rec.Body.String())
	}
	if rec := s.call(inboundGlobalRoutes[1], &s.global, s.a.ID); rec.Code != http.StatusSeeOther {
		t.Errorf("POST /inbound/domains/{id}/delete as the global role = %d:\n%s", rec.Code, rec.Body.String())
	}
}

// The list is filtered in the query: a domain administrator sees the inbound
// domains assigned to them and nothing that lets them add or delete one.
func TestInboundListShowsOnlyTheAssignedDomains(t *testing.T) {
	s := newInboundStand(t)
	list := func(p *auth.Principal) *httptest.ResponseRecorder {
		return send(s.h.HandleInboundList, p, "GET", "/inbound/domains", nil, nil)
	}

	rec := list(&s.ownsA)
	if rec.Code != http.StatusOK {
		t.Fatalf("list as the administrator of A = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "a.example.com") || strings.Contains(body, "b.example.com") {
		t.Errorf("the administrator of A must see A and not B:\n%s", body)
	}
	for _, globalOnly := range []string{"Add inbound domain", "/delete"} {
		if strings.Contains(body, globalOnly) {
			t.Errorf("the list offers a domain administrator %q", globalOnly)
		}
	}

	// All covers a domain added after the user was given it.
	if _, err := s.h.store.AddInboundDomain("later.example.com"); err != nil {
		t.Fatal(err)
	}
	body = list(&s.allIn).Body.String()
	for _, want := range []string{"a.example.com", "b.example.com", "later.example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("All inbound does not list %s", want)
		}
	}
	if strings.Contains(list(&s.ownsA).Body.String(), "later.example.com") {
		t.Error("a domain added later appeared for a user who was assigned A only")
	}

	for who, p := range map[string]*auth.Principal{"outbound reach only": &s.outboundOnly, "no principal": nil} {
		if rec := list(p); rec.Code != http.StatusNotFound {
			t.Errorf("list with %s = %d, want 404", who, rec.Code)
		}
	}
	if body := list(&s.global).Body.String(); !strings.Contains(body, "Add inbound domain") || !strings.Contains(body, "b.example.com") {
		t.Errorf("the global role lost the full list or the add form:\n%s", body)
	}
}

// Losing the last assigned inbound domain (it was deleted) takes the group away
// as a whole: the list answers 404, like for a user who never had inbound.
func TestInboundListWithNothingLeftIs404(t *testing.T) {
	s := newInboundStand(t)
	if err := s.h.store.DeleteInboundDomain(s.a.ID); err != nil {
		t.Fatal(err)
	}
	u, err := s.h.store.GetUser(s.ownsA.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := auth.Principal{ID: u.ID, Username: u.Username, Role: u.Role, InboundDomains: u.InboundDomainIDs}
	if rec := send(s.h.HandleInboundList, &p, "GET", "/inbound/domains", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("list after the only assigned inbound domain was deleted = %d, want 404", rec.Code)
	}
}

// The outbound side of the same rule, route by route: a domain administrator
// asking for another tenant's sending domain or application by id gets 404 —
// also when the application is addressed through the caller's own domain —
// and so does a user whose reach is inbound only.
func TestOutboundRoutesAnswerAnotherTenant404(t *testing.T) {
	h, domains := serverWithTwoDomains(t)
	h.domains = domain.NewService(h.store, nil, nil, "mail")
	h.apps = app.NewService(h.store, nil, nil)
	first, second := domains["first.example.ru"], domains["second.example.ru"]
	foreignApp, err := h.store.GetApplicationByLogin("second-app")
	if err != nil {
		t.Fatal(err)
	}
	in, err := h.store.AddInboundDomain("second.example.ru")
	if err != nil {
		t.Fatal(err)
	}
	ownsFirst := userWith(t, h.store, "owns-first", store.RoleDomain, store.Reach{DomainIDs: []int64{first.ID}})
	inboundOnly := userWith(t, h.store, "inbound-only", store.RoleDomain, store.Reach{InboundDomainIDs: []int64{in.ID}})

	did := map[string]string{"id": idStr(second.ID)}
	// Another tenant's application, addressed through its own domain — and
	// through the caller's own domain, which it is not in: the URL names a
	// domain the caller may open, but the application still is not theirs.
	aid := map[string]string{"id": idStr(second.ID), "aid": idStr(foreignApp.ID)}
	smuggled := map[string]string{"id": idStr(first.ID), "aid": idStr(foreignApp.ID)}
	type outRoute struct {
		method, target string
		handler        http.HandlerFunc
		pathValues     map[string]string
	}
	routes := []outRoute{
		{"GET", "/outbound/domains/{id}", h.HandleDomainDetail, did},
		{"GET", "/outbound/domains/{id}/settings", h.HandleDomainSettings, did},
		{"POST", "/outbound/domains/{id}/dns-recheck", h.HandleDomainDNSRecheck, did},
		{"POST", "/outbound/domains/{id}/settings/ratelimit", h.HandleDomainRateLimit, did},
		{"POST", "/outbound/domains/{id}/settings/ratelimit/recalc", h.HandleDomainRateLimitRecalc, did},
		{"POST", "/outbound/domains/{id}/settings/reports", h.HandleDomainDMARC, did},
		{"POST", "/outbound/domains/{id}/settings/export", h.HandleExportDomain, did},
		{"GET", "/outbound/domains/{id}/applications/new", h.HandleApplicationNew, did},
		{"POST", "/outbound/domains/{id}/applications/new", h.HandleApplicationCreate, did},
	}
	for _, pv := range []map[string]string{aid, smuggled} {
		routes = append(routes,
			outRoute{"GET", "/outbound/domains/{id}/applications/{aid}", h.HandleApplicationEdit, pv},
			outRoute{"POST", "/outbound/domains/{id}/applications/{aid}", h.HandleApplicationSave, pv},
			outRoute{"POST", "/outbound/domains/{id}/applications/{aid}/password", h.HandleRegenPassword, pv},
			outRoute{"POST", "/outbound/domains/{id}/applications/{aid}/ratelimit/recalc", h.HandleAppRateLimitRecalc, pv},
			outRoute{"POST", "/outbound/domains/{id}/applications/{aid}/delete", h.HandleDeleteApplication, pv},
		)
	}
	for _, rt := range routes {
		for who, p := range map[string]*auth.Principal{
			"the administrator of another sending domain":    &ownsFirst,
			"an inbound-only administrator of the same name": &inboundOnly,
			"no principal": nil,
		} {
			rec := send(rt.handler, p, rt.method, rt.target, rt.pathValues, url.Values{})
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s on another tenant's id as %s = %d, want 404:\n%s", rt.method, rt.target, who, rec.Code, rec.Body.String())
			}
		}
	}

	// Nothing was changed by the refused requests.
	if _, err := h.store.GetApplicationByLogin("second-app"); err != nil {
		t.Errorf("the foreign application did not survive the refused requests: %v", err)
	}
	apps, err := h.store.ListApplicationsByDomain(second.ID)
	if err != nil || len(apps) != 1 {
		t.Errorf("the foreign domain has %d applications after the refused requests (%v), want 1", len(apps), err)
	}
}

// The user form carries two lists. A domain administrator needs something on
// at least one of them; All wins over the rows under it; promoting to the
// global role drops the reach.
func TestUserFormAssignsBothDirections(t *testing.T) {
	h, st := inboundHandlers(t)
	out, _ := st.AddDomain("out.example.com", "mail")
	in, _ := st.AddInboundDomain("in.example.com")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	const password = "a-long-enough-password-1"

	create := func(username string, extra url.Values) (*httptest.ResponseRecorder, store.User) {
		t.Helper()
		form := url.Values{"username": {username}, "password": {password}, "role": {"domain"}}
		for k, v := range extra {
			form[k] = v
		}
		rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, form)
		u, _ := st.GetUserByUsername(username)
		return rec, u
	}

	rec, u := create("nothing", nil)
	if rec.Code != http.StatusBadRequest || u.ID != 0 {
		t.Errorf("a domain administrator with nothing assigned = %d, user created: %v", rec.Code, u.ID != 0)
	}

	rec, u = create("inbound-only", url.Values{"inbound_domain_ids": {idStr(in.ID)}})
	if rec.Code != http.StatusSeeOther || len(u.InboundDomainIDs) != 1 || len(u.DomainIDs) != 0 || u.AllDomains || u.AllInboundDomains {
		t.Errorf("inbound only = %d, %+v:\n%s", rec.Code, u, rec.Body.String())
	}

	// All ticked together with rows: the rows are ignored, not stored.
	rec, u = create("all-out", url.Values{"all_domains": {"1"}, "domain_ids": {idStr(out.ID)}})
	if rec.Code != http.StatusSeeOther || !u.AllDomains || len(u.DomainIDs) != 0 {
		t.Errorf("All outbound = %d, %+v", rec.Code, u)
	}

	update := func(u store.User, form url.Values) store.User {
		t.Helper()
		form.Set("username", u.Username)
		rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)}, form)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("update %s = %d:\n%s", u.Username, rec.Code, rec.Body.String())
		}
		got, err := st.GetUser(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// Both lists at once; then unticking All brings no stale row back.
	u = update(u, url.Values{"role": {"domain"}, "domain_ids": {idStr(out.ID)}, "all_inbound_domains": {"1"}})
	if u.AllDomains || len(u.DomainIDs) != 1 || !u.AllInboundDomains || len(u.InboundDomainIDs) != 0 {
		t.Errorf("after the update: %+v", u)
	}

	// With inbound switched off the form has no inbound list; saving it must
	// not wipe what the user had there.
	h.cfg.InboundEnabled = false
	u = update(u, url.Values{"role": {"domain"}, "domain_ids": {idStr(out.ID)}})
	if !u.AllInboundDomains {
		t.Errorf("saving the form with inbound off dropped the user's inbound reach: %+v", u)
	}
	h.cfg.InboundEnabled = true

	// Emptying both lists is refused and changes nothing.
	rec = send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {u.Username}, "role": {"domain"}})
	if after, _ := st.GetUser(u.ID); rec.Code != http.StatusBadRequest || after.Reach().Empty() {
		t.Errorf("emptying both lists = %d, reach now %+v", rec.Code, after.Reach())
	}

	// Promotion drops the reach, so a later demotion starts from nothing.
	u = update(u, url.Values{"role": {"global"}})
	if u.Role != store.RoleGlobal || !u.Reach().Empty() {
		t.Errorf("after promotion: role %s, reach %+v", u.Role, u.Reach())
	}

	// The form shows both lists with what is ticked.
	other, _ := st.GetUserByUsername("inbound-only")
	body := send(h.HandleUserEdit, &root, "GET", "/server/users/"+idStr(other.ID), map[string]string{"uid": idStr(other.ID)}, nil).Body.String()
	for _, want := range []string{
		`name="all_domains"`, `name="all_inbound_domains"`, "out.example.com",
		`name="inbound_domain_ids" value="` + idStr(in.ID) + `" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the user form is missing %q", want)
		}
	}
}
