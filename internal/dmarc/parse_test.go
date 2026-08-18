package dmarc

import (
	"bytes"
	"compress/gzip"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

const sampleXML = `<?xml version="1.0"?>
<feedback>
  <report_metadata>
    <org_name>google.com</org_name>
    <email>noreply@google.com</email>
    <report_id>12345</report_id>
    <date_range><begin>1723593600</begin><end>1723680000</end></date_range>
  </report_metadata>
  <policy_published>
    <domain>example.com</domain>
    <p>none</p><sp>none</sp><pct>100</pct>
    <adkim>r</adkim><aspf>r</aspf>
  </policy_published>
  <record>
    <row>
      <source_ip>203.0.113.10</source_ip>
      <count>10</count>
      <policy_evaluated>
        <disposition>none</disposition>
        <dkim>pass</dkim><spf>pass</spf>
      </policy_evaluated>
    </row>
    <identifiers><header_from>example.com</header_from></identifiers>
  </record>
  <record>
    <row>
      <source_ip>198.51.100.1</source_ip>
      <count>2</count>
      <policy_evaluated>
        <disposition>none</disposition>
        <dkim>fail</dkim><spf>fail</spf>
      </policy_evaluated>
    </row>
    <identifiers><header_from>example.com</header_from></identifiers>
  </record>
</feedback>`

func TestParseAggregateGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(sampleXML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := ParseAggregate(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseAggregate: %v", err)
	}
	if got.Domain != "example.com" || got.Reporter != "google.com" {
		t.Fatalf("metadata = %+v", got)
	}
	if got.PassCount != 10 || got.FailCount != 2 {
		t.Fatalf("counts = %d pass %d fail", got.PassCount, got.FailCount)
	}
	if !got.PeriodBegin.Equal(time.Unix(1723593600, 0).UTC()) {
		t.Fatalf("period begin = %v", got.PeriodBegin)
	}
}

func TestIngestMessageStoresReport(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(sampleXML))
	_ = zw.Close()

	msg := bytes.NewBufferString(
		"From: noreply@google.com\r\n" +
			"To: dmarc-reports@mail.example.com\r\n" +
			"Subject: Report\r\n" +
			"Content-Type: application/gzip; name=\"report.xml.gz\"\r\n" +
			"\r\n",
	)
	// append raw gzip for simple single-part test
	msg.Write(gz.Bytes())

	if err := IngestMessage(st, msg, "dmarc-reports@mail.example.com", time.Now().UTC()); err != nil {
		t.Fatalf("IngestMessage: %v", err)
	}
	list, err := st.ListDMARCReports(nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("reports = %d", len(list))
	}
	if list[0].PassCount != 10 {
		t.Fatalf("pass = %d", list[0].PassCount)
	}
}

func TestHostedReportAddress(t *testing.T) {
	got := HostedReportAddress("mail.example.com", "Example.COM")
	want := "dmarc-reports+example.com@mail.example.com"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
