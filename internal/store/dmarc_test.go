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
	list, err := st.ListDMARCReports([]string{"example.com"}, 10)
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
	list, _ := st.ListDMARCReports(nil, 100)
	if len(list) != 0 {
		t.Fatalf("expected prune by age, got %d", len(list))
	}
}
