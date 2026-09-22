package store

import (
	"database/sql"
	"testing"
)

func TestDomainDMARCRua(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDomain("example.com", "sel")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	got, err := st.GetDomain(d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.DMARCRua.Valid {
		t.Fatal("new domain should inherit profile")
	}

	if err := st.UpdateDomainDMARCRua(d.ID, sql.NullString{Valid: true, String: "reports@hub.com"}); err != nil {
		t.Fatalf("UpdateDomainDMARCRua custom: %v", err)
	}
	got, err = st.GetDomain(d.ID)
	if err != nil || got.DMARCRua.String != "reports@hub.com" {
		t.Fatalf("custom = %+v, err=%v", got.DMARCRua, err)
	}

	if err := st.UpdateDomainDMARCRua(d.ID, sql.NullString{Valid: true}); err != nil {
		t.Fatalf("UpdateDomainDMARCRua none: %v", err)
	}
	got, _ = st.GetDomain(d.ID)
	if !got.DMARCRua.Valid || got.DMARCRua.String != "" {
		t.Fatalf("none = %+v", got.DMARCRua)
	}

	if err := st.UpdateDomainDMARCRua(d.ID, sql.NullString{}); err != nil {
		t.Fatalf("UpdateDomainDMARCRua inherit: %v", err)
	}
	got, _ = st.GetDomain(d.ID)
	if got.DMARCRua.Valid {
		t.Fatalf("inherit = %+v", got.DMARCRua)
	}
}
