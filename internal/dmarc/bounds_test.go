package dmarc

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

func reportXML(orgName, domain, records string) string {
	return `<?xml version="1.0"?><feedback><report_metadata><org_name>` + orgName +
		`</org_name><email>a@example.net</email><report_id>1</report_id>` +
		`<date_range><begin>1723593600</begin><end>1723680000</end></date_range></report_metadata>` +
		`<policy_published><domain>` + domain + `</domain><p>none</p></policy_published>` + records + `</feedback>`
}

const oneRecord = `<record><row><source_ip>203.0.113.10</source_ip><count>3</count>` +
	`<policy_evaluated><disposition>none</disposition><dkim>pass</dkim><spf>pass</spf></policy_evaluated></row>` +
	`<identifiers><header_from>example.com</header_from></identifiers></record>`

// The XML is a stranger's: what it may make the relay store is bounded.
func TestParseAggregateBoundsWhatAReportHolds(t *testing.T) {
	long := strings.Repeat("é", 4000)
	got, err := ParseAggregate([]byte(reportXML(long, "example.com", oneRecord)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reporter) > maxFieldLen || !strings.HasPrefix(long, got.Reporter) || got.Reporter == "" {
		t.Fatalf("reporter name is %d bytes, want the start of the name in at most %d", len(got.Reporter), maxFieldLen)
	}
	if strings.ContainsRune(got.Reporter, '�') {
		t.Fatal("the name was cut in the middle of a character")
	}

	huge := `<record><row><source_ip>203.0.113.10</source_ip><count>9223372036854775807</count>` +
		`<policy_evaluated><dkim>pass</dkim></policy_evaluated></row></record>`
	got, err = ParseAggregate([]byte(reportXML("r", "example.com", huge+huge)))
	if err != nil {
		t.Fatal(err)
	}
	if got.PassCount != 2*maxRecordCount {
		t.Fatalf("pass count = %d, want two records clamped to %d each", got.PassCount, maxRecordCount)
	}

	if _, err := ParseAggregate([]byte(reportXML("r", "example.com", strings.Repeat(oneRecord, maxReportRecords)))); err != nil {
		t.Fatalf("a report of %d records was refused: %v", maxReportRecords, err)
	}
	if _, err := ParseAggregate([]byte(reportXML("r", "example.com", strings.Repeat(oneRecord, maxReportRecords+1)))); err == nil {
		t.Fatalf("a report of %d records was accepted", maxReportRecords+1)
	}
}

// A report over the bound is one the relay does not keep: dropped, not retried.
func TestIngestMessageRejectsAReportWithTooManyRecords(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddDomain("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	xml := reportXML("r", "example.com", strings.Repeat(oneRecord, maxReportRecords+1))
	err = IngestMessage(st, strings.NewReader(gzipMessage(t, xml)), "dmarc-reports+example.com@mail.example.com", time.Now().UTC())
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}

// Reports filed under one domain do not push another domain's reports out.
func TestIngestKeepsEachDomainsReportsApart(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	if _, err := st.InsertDMARCReport(store.DMARCReport{
		Domain: "quiet.example", Reporter: "genuine", ReportID: "1", PeriodBegin: now, PeriodEnd: now, ReceivedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < store.DMARCReportsMaxKeep+20; i++ {
		if _, err := st.InsertDMARCReport(store.DMARCReport{
			Domain: "flooded.example", Reporter: "stranger", ReportID: strings.Repeat("x", 1+i), PeriodBegin: now, PeriodEnd: now, ReceivedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PruneDMARCReports("flooded.example"); err != nil {
		t.Fatal(err)
	}
	quiet, err := st.ListDMARCReports(store.DMARCReportScope{Domains: []string{"quiet.example"}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet) != 1 {
		t.Fatalf("the other domain has %d reports after the flood, want its 1", len(quiet))
	}
	flooded, err := st.ListDMARCReports(store.DMARCReportScope{Domains: []string{"flooded.example"}}, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(flooded) > store.DMARCReportsMaxKeep {
		t.Fatalf("the flooded domain keeps %d reports, want at most %d", len(flooded), store.DMARCReportsMaxKeep)
	}
}
