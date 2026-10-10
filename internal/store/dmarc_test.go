package store

import (
	"testing"
	"time"
)

func TestDMARCReportRoundTrip(t *testing.T) {
	st, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC().Truncate(time.Second)
	_, err = st.InsertDMARCReport(DMARCReport{
		Domain:      "example.com",
		Reporter:    "google.com",
		ReportID:    "abc",
		PeriodBegin: now.Add(-24 * time.Hour),
		PeriodEnd:   now,
		ReceivedAt:  now,
		PassCount:   5,
		FailCount:   1,
		Records: []DMARCReportRecord{{
			SourceIP: "203.0.113.1", Count: 5, SPFResult: "pass", DKIMResult: "pass",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.ListDMARCReports(DMARCReportScope{Domains: []string{"example.com"}}, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v err=%v", list, err)
	}
	got, err := st.GetDMARCReport(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PassCount != 5 || len(got.Records) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestPruneDMARCReports(t *testing.T) {
	st, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	old := time.Now().UTC().AddDate(0, 0, -120)
	for i := 0; i < 3; i++ {
		if _, err := st.InsertDMARCReport(DMARCReport{
			Domain: "example.com", Reporter: "r", ReportID: string(rune('a' + i)),
			PeriodBegin: old, PeriodEnd: old, ReceivedAt: old,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PruneDMARCReports(); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListDMARCReports(DMARCReportScope{AllDomains: true}, 100)
	if len(list) != 0 {
		t.Fatalf("expected prune by age, got %d", len(list))
	}
}

// The domains a list is limited to are the whole answer: an empty list is a
// viewer who reaches nothing, never a viewer who reaches everything.
func TestListDMARCReportsEmptyListIsNoRows(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().UTC()
	for _, domain := range []string{"a.example", "b.example"} {
		if _, err := st.InsertDMARCReport(DMARCReport{
			Domain: domain, Reporter: "r", ReportID: domain,
			PeriodBegin: now.Add(-24 * time.Hour), PeriodEnd: now, ReceivedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	for name, scope := range map[string]DMARCReportScope{
		"an empty list":   {Domains: []string{}},
		"no scope at all": {},
	} {
		list, err := st.ListDMARCReports(scope, 10)
		if err != nil {
			t.Fatalf("%s is an error: %v", name, err)
		}
		if len(list) != 0 {
			t.Errorf("%s returned %d report(s), want none", name, len(list))
		}
	}
	list, err := st.ListDMARCReports(DMARCReportScope{Domains: []string{"a.example"}}, 10)
	if err != nil || len(list) != 1 || list[0].Domain != "a.example" {
		t.Errorf("one domain = %+v, %v; want only a.example", list, err)
	}
	list, err = st.ListDMARCReports(DMARCReportScope{AllDomains: true}, 10)
	if err != nil || len(list) != 2 {
		t.Errorf("all domains = %d report(s), %v; want both", len(list), err)
	}
	list, err = st.ListDMARCReports(DMARCReportScope{AllDomains: true, Domains: []string{"a.example"}}, 10)
	if err != nil || len(list) != 2 {
		t.Errorf("all domains with a list = %d report(s), %v; want both (AllDomains wins)", len(list), err)
	}
}
