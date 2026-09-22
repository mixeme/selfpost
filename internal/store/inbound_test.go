package store

import (
	"errors"
	"testing"
)

func TestInboundDomainCRUD(t *testing.T) {
	st := openTestStore(t)

	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatalf("AddInboundDomain: %v", err)
	}
	if d.ID == 0 || d.Name != "lists.example.com" || d.RecipientMode != RecipientModeList {
		t.Fatalf("unexpected domain: %+v", d)
	}
	if d.Port != 25 || d.TLSMode != TLSModeMay || d.Host != "" {
		t.Fatalf("unexpected default transport: %+v", d)
	}

	if _, err := st.AddInboundDomain("lists.example.com"); !errors.Is(err, ErrInboundDomainExists) {
		t.Fatalf("duplicate error = %v, want ErrInboundDomainExists", err)
	}

	if err := st.UpdateInboundTransport(d.ID, "10.0.0.8", 25, TLSModeEncrypt); err != nil {
		t.Fatalf("UpdateInboundTransport: %v", err)
	}
	addrs := []string{"staff@lists.example.com", "postmaster@lists.example.com"}
	if err := st.UpdateInboundRecipients(d.ID, RecipientModeList, addrs); err != nil {
		t.Fatalf("UpdateInboundRecipients: %v", err)
	}

	got, err := st.GetInboundDomain(d.ID)
	if err != nil {
		t.Fatalf("GetInboundDomain: %v", err)
	}
	if got.Host != "10.0.0.8" || got.TLSMode != TLSModeEncrypt || got.RecipientCount != 2 {
		t.Fatalf("get after update: %+v", got)
	}
	if len(got.Recipients) != 2 || got.Recipients[0] != "postmaster@lists.example.com" {
		t.Fatalf("recipients not sorted: %v", got.Recipients)
	}

	list, err := st.ListInboundDomains()
	if err != nil {
		t.Fatalf("ListInboundDomains: %v", err)
	}
	if len(list) != 1 || list[0].RecipientCount != 2 {
		t.Fatalf("list: %+v", list)
	}

	if err := st.UpdateInboundRecipients(d.ID, RecipientModeAny, nil); err != nil {
		t.Fatalf("switch to any: %v", err)
	}
	got, err = st.GetInboundDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecipientMode != RecipientModeAny || got.RecipientCount != 0 || len(got.Recipients) != 0 {
		t.Fatalf("any mode should clear the list: %+v", got)
	}

	if err := st.DeleteInboundDomain(d.ID); err != nil {
		t.Fatalf("DeleteInboundDomain: %v", err)
	}
	assertCount(t, st, "inbound_domains", 0)
	assertCount(t, st, "inbound_transports", 0)
	assertCount(t, st, "inbound_recipients", 0)
	if _, err := st.GetInboundDomain(d.ID); !errors.Is(err, ErrInboundDomainNotFound) {
		t.Fatalf("Get after delete = %v, want ErrInboundDomainNotFound", err)
	}
}

func TestInboundDomainNotFound(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.GetInboundDomain(99); !errors.Is(err, ErrInboundDomainNotFound) {
		t.Fatalf("GetInboundDomain(missing) = %v", err)
	}
	if err := st.UpdateInboundTransport(99, "10.0.0.1", 25, TLSModeNone); !errors.Is(err, ErrInboundDomainNotFound) {
		t.Fatalf("UpdateInboundTransport(missing) = %v", err)
	}
	if err := st.DeleteInboundDomain(99); !errors.Is(err, ErrInboundDomainNotFound) {
		t.Fatalf("DeleteInboundDomain(missing) = %v", err)
	}
}

func TestInboundDeleteCascadesRecipients(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddInboundDomain("backup.example.net")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateInboundRecipients(d.ID, RecipientModeList, []string{"a@backup.example.net"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteInboundDomain(d.ID); err != nil {
		t.Fatal(err)
	}
	assertCount(t, st, "inbound_recipients", 0)
}
