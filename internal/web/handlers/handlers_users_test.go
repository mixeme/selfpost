package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// The users form's writes, driven through the handlers (audit 2026-10-12, M7
// and M9: the save is one store call now, and these pin what it does).

// A save that changes name, e-mail and role lands whole; promoted to global
// the user keeps no assignment.
func TestUserFormSavesTheWholeChange(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "shop", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"shop2"}, "email": {"shop@example.com"}, "role": {"global"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, err := st.GetUser(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Username != "shop2" || after.Email != "shop@example.com" || after.Role != store.RoleGlobal || !after.Reach().Empty() {
		t.Fatalf("after the save: %+v", after)
	}

	// And back to a domain user, with an assignment.
	rec = send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"shop2"}, "email": {"shop@example.com"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ = st.GetUser(u.ID)
	if after.Role != store.RoleDomain || len(after.DomainIDs) != 1 || after.DomainIDs[0] != out1.ID {
		t.Fatalf("demoted: %+v", after)
	}
}

// The last global administrator is not demoted, and a refused save changes
// nothing - not the name typed in the same form either.
func TestUserFormKeepsTheLastGlobal(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(root.ID), map[string]string{"uid": idStr(root.ID)},
		url.Values{"username": {"renamed"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "You cannot demote yourself without another global administrator.") {
		t.Fatalf("demoting the last global = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ := st.GetUser(root.ID)
	if after.Username != "root" || after.Role != store.RoleGlobal {
		t.Fatalf("the refused save changed the user: %+v", after)
	}
}

// A domain user must keep something assigned; the refusal changes nothing.
func TestUserFormRefusesADomainUserWithNothingAssigned(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "shop", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"renamed"}, "role": {"domain"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Assign at least one outbound or inbound domain") {
		t.Fatalf("nothing assigned = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ := st.GetUser(u.ID)
	if after.Username != "shop" || len(after.DomainIDs) != 1 {
		t.Fatalf("the refused save changed the user: %+v", after)
	}
}

// A user is created with the e-mail and the assignment in the one save.
func TestUserFormCreatesWithEmailAndReach(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil,
		url.Values{"username": {"fresh"}, "password": {"a-long-enough-password"}, "email": {"fresh@example.com"},
			"role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d:\n%s", rec.Code, rec.Body.String())
	}
	u, err := st.GetUserByUsername("fresh")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "fresh@example.com" || u.Role != store.RoleDomain || len(u.DomainIDs) != 1 {
		t.Fatalf("created %+v", u)
	}
}

// A domain deleted and added again under its old name starts with an empty
// log for whoever it is assigned to; its earlier rows are the global role's
// alone (audit 2026-10-12, L12).
func TestADomainAddedAgainShowsItsAssigneeNothingOld(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	old, _ := st.AddDomain("shop.example", "mail")
	st.AddApplication(old.ID, "shop", store.AddressModeWildcard, nil)
	if err := st.InsertQueued(store.SendLogEntry{QueueID: "OLDQ", Domain: "shop.example", AppLogin: "shop", To: "a@example.net", Subject: "old mail"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteDomain(old.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := st.AddDomain("shop.example", "mail")
	st.AddApplication(again.ID, "shop", store.AddressModeWildcard, nil)
	if err := st.InsertQueued(store.SendLogEntry{QueueID: "NEWQ", Domain: "shop.example", AppLogin: "shop", To: "a@example.net", Subject: "new mail"}); err != nil {
		t.Fatal(err)
	}
	owner := userWith(t, st, "owner", store.RoleDomain, store.Reach{DomainIDs: []int64{again.ID}})
	rows, _ := st.QuerySendLog(store.SendLogFilter{AllDomains: true}, 10, 0)
	var oldID, newID int64
	for _, r := range rows {
		if r.QueueID == "OLDQ" {
			oldID = r.ID
		} else {
			newID = r.ID
		}
	}

	page := func(p *auth.Principal, id int64) int {
		return send(h.HandleDelivery, p, "GET", "/outbound/log/"+idStr(id), map[string]string{"id": idStr(id)}, nil).Code
	}
	if code := page(&owner, oldID); code != http.StatusNotFound {
		t.Errorf("the old row's page for the new assignee = %d, want 404", code)
	}
	if code := page(&owner, newID); code != http.StatusOK {
		t.Errorf("the new row's page for its assignee = %d, want 200", code)
	}
	if code := page(&root, oldID); code != http.StatusOK {
		t.Errorf("the old row's page for the global role = %d, want 200", code)
	}
	body := send(h.HandleDeliveries, &owner, "GET", "/outbound/log", nil, nil).Body.String()
	if strings.Contains(body, "old mail") || !strings.Contains(body, "new mail") {
		t.Errorf("the new assignee's log shows the old row or misses the new one")
	}
	body = send(h.HandleDeliveries, &root, "GET", "/outbound/log", nil, nil).Body.String()
	if !strings.Contains(body, "old mail") || !strings.Contains(body, "new mail") {
		t.Errorf("the global log is missing a row")
	}
}

// A refused message's page shows what Postfix wrote about the refusal: the
// NOQUEUE line put on the row of a refusal at MAIL FROM, or, for a row from
// before that line was kept, a sentence that says so (audit 2026-10-12,
// stage 2b).
func TestARefusedMessageShowsPostfixsLine(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	d, _ := st.AddDomain("example.org", "mail")
	st.AddApplication(d.ID, "alerts", store.AddressModeWildcard, nil)
	for _, subject := range []string{"with its line", "from before"} {
		if err := st.InsertRejected(store.SendLogEntry{Domain: "example.org", AppLogin: "alerts", From: "alerts@example.org", Subject: subject, Reason: store.ReasonLimit}); err != nil {
			t.Fatal(err)
		}
		if subject == "with its line" {
			line := time.Now().UTC().Format("2006-01-02T15:04:05Z") + " mail postfix/smtpd[2131]: NOQUEUE: milter-reject: MAIL from unknown[198.51.100.7]: 451 4.7.1 Service unavailable - try again later; from=<alerts@example.org> proto=ESMTP helo=<app>"
			if ok, err := st.AttachRefusalLine("alerts", "alerts@example.org", time.Now(), line); err != nil || !ok {
				t.Fatalf("attach = %v, %v", ok, err)
			}
		}
	}
	rows, _ := st.QuerySendLog(store.SendLogFilter{AllDomains: true}, 10, 0)
	for _, r := range rows {
		body := send(h.HandleDelivery, &root, "GET", "/outbound/log/"+idStr(r.ID), map[string]string{"id": idStr(r.ID)}, nil).Body.String()
		if r.Subject == "with its line" {
			if !strings.Contains(body, `<span class="sp-w">mail postfix/smtpd[2131]: NOQUEUE: milter-reject: MAIL from unknown[198.51.100.7]: 451 4.7.1`) {
				t.Errorf("the page of a refusal with its line lacks it:\n%s", body)
			}
			continue
		}
		if !strings.Contains(body, "the message was refused before version 2.5.1, or the line was never read") {
			t.Errorf("the page of a refusal without its line does not say why")
		}
	}
}

// A message the reconcile sweep closed does not say a receiving server
// refused it: neither did (audit 2026-10-12, stage 2b).
func TestAMessageTheSweepClosedSaysSo(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	if err := st.InsertQueued(store.SendLogEntry{QueueID: "GONEQ", Domain: "example.org", AppLogin: "a", To: "x@example.net", Subject: "lost"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.QuerySendLog(store.SendLogFilter{AllDomains: true}, 1, 0)
	if _, err := st.CloseUnreported(rows[0].ID); err != nil {
		t.Fatal(err)
	}
	body := send(h.HandleDelivery, &root, "GET", "/outbound/log/"+idStr(rows[0].ID), map[string]string{"id": idStr(rows[0].ID)}, nil).Body.String()
	if !strings.Contains(body, "Closed by the panel") || strings.Contains(body, "the receiving server refused") {
		t.Fatalf("the page of a row the sweep closed:\n%s", body)
	}
}
