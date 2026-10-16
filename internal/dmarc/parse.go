package dmarc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ParsedReport is the structured form of a DMARC aggregate XML payload.
type ParsedReport struct {
	Reporter     string
	ReportID     string
	ContactEmail string
	PeriodBegin  time.Time
	PeriodEnd    time.Time
	Domain       string
	PolicyP      string
	PolicySP     string
	PolicyPct    int
	PolicyADKIM  string
	PolicyASPF   string
	Records      []ParsedRecord
	PassCount    int
	FailCount    int
}

// ParsedRecord is one <record> row from the aggregate XML.
type ParsedRecord struct {
	SourceIP    string
	Count       int
	Disposition string
	SPFResult   string
	DKIMResult  string
	HeaderFrom  string
}

// feedbackXML follows RFC 7489, Appendix C. The schema's targetNamespace is
// "http://dmarc.org/dmarc-xml/0.1"; a tag without a namespace matches the
// local name in any namespace (Go, src/encoding/xml/read.go: the namespace is
// compared only when the tag names one), so a report that declares one is
// read the same.
type feedbackXML struct {
	XMLName  xml.Name `xml:"feedback"`
	Metadata struct {
		OrgName   string `xml:"org_name"`
		Email     string `xml:"email"`
		ReportID  string `xml:"report_id"`
		DateRange struct {
			Begin int64 `xml:"begin"`
			End   int64 `xml:"end"`
		} `xml:"date_range"`
	} `xml:"report_metadata"`
	Policy struct {
		Domain string  `xml:"domain"`
		P      string  `xml:"p"`
		SP     string  `xml:"sp"`
		Pct    *string `xml:"pct"`
		ADKIM  string  `xml:"adkim"`
		ASPF   string  `xml:"aspf"`
	} `xml:"policy_published"`
	Records []struct {
		Row struct {
			SourceIP string `xml:"source_ip"`
			Count    int    `xml:"count"`
			Policy   struct {
				Disposition string `xml:"disposition"`
				DKIM        string `xml:"dkim"`
				SPF         string `xml:"spf"`
			} `xml:"policy_evaluated"`
		} `xml:"row"`
		Identifiers struct {
			HeaderFrom string `xml:"header_from"`
		} `xml:"identifiers"`
	} `xml:"record"`
}

// What a report may hold. The XML comes from anyone who knows a report address,
// and eight megabytes of it (maxAggregateXML) fit some 160 000 empty records or
// one name of eight megabytes - every one of them a row or a table cell later.
// A real report has tens of records, a few thousand from the largest
// receivers, and no field longer than a line - an estimate: the live
// installation had received one report by 2026-10-15, of one record, which
// says nothing yet about the largest (assumptions C-M8: keep this ten times
// above the largest seen).
const (
	maxReportRecords = 5000
	maxFieldLen      = 255
	maxRecordCount   = 1_000_000_000
)

// field trims a text value of the report and cuts it to maxFieldLen bytes, on
// a character boundary.
func field(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxFieldLen {
		return s
	}
	s = s[:maxFieldLen]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// ParseAggregate decodes gzip- or zip-compressed, or raw, DMARC aggregate XML.
func ParseAggregate(raw []byte) (ParsedReport, error) {
	data, err := decompress(raw)
	if err != nil {
		return ParsedReport{}, err
	}
	var doc feedbackXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return ParsedReport{}, fmt.Errorf("dmarc xml: %w", err)
	}
	if doc.Metadata.ReportID == "" || doc.Policy.Domain == "" {
		return ParsedReport{}, fmt.Errorf("dmarc xml: missing report_id or domain")
	}
	if len(doc.Records) > maxReportRecords {
		return ParsedReport{}, fmt.Errorf("dmarc xml: %d records, more than the %d a report may hold", len(doc.Records), maxReportRecords)
	}
	// Cut, not refused: a domain name is at most 253 bytes, so a longer one
	// names nothing this relay sends for and is turned away as such.
	out := ParsedReport{
		Reporter:     field(doc.Metadata.OrgName),
		ReportID:     field(doc.Metadata.ReportID),
		ContactEmail: field(doc.Metadata.Email),
		PeriodBegin:  time.Unix(doc.Metadata.DateRange.Begin, 0).UTC(),
		PeriodEnd:    time.Unix(doc.Metadata.DateRange.End, 0).UTC(),
		Domain:       strings.ToLower(field(doc.Policy.Domain)),
		PolicyP:      field(doc.Policy.P),
		PolicySP:     field(doc.Policy.SP),
		PolicyADKIM:  field(doc.Policy.ADKIM),
		PolicyASPF:   field(doc.Policy.ASPF),
	}
	// RFC 7489 § 6.3: pct is "between 0 and 100, inclusive", 100 when absent.
	// An explicit 0 is a policy applied to no message, and stays 0; a report
	// without the element, or with an empty or unreadable one, gets the
	// default, and a value outside the range is held to it (assumptions
	// C-M7; review of stage 2c, F9).
	out.PolicyPct = 100
	if doc.Policy.Pct != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(*doc.Policy.Pct)); err == nil {
			out.PolicyPct = min(max(n, 0), 100)
		}
	}
	for _, rec := range doc.Records {
		row := ParsedRecord{
			SourceIP:    field(rec.Row.SourceIP),
			Count:       rec.Row.Count,
			Disposition: field(rec.Row.Policy.Disposition),
			SPFResult:   field(rec.Row.Policy.SPF),
			DKIMResult:  field(rec.Row.Policy.DKIM),
			HeaderFrom:  field(rec.Identifiers.HeaderFrom),
		}
		if row.Count <= 0 {
			row.Count = 1
		}
		// Bounded so that the sums of a report cannot wrap around.
		if row.Count > maxRecordCount {
			row.Count = maxRecordCount
		}
		if dmarcRecordPasses(row) {
			out.PassCount += row.Count
		} else {
			out.FailCount += row.Count
		}
		out.Records = append(out.Records, row)
	}
	return out, nil
}

// dmarcRecordPasses: RFC 7489, Appendix C, PolicyEvaluatedType - dkim and spf
// are "The DMARC-aligned authentication result"; § 4.2: DMARC passes when one
// supported mechanism passes aligned.
func dmarcRecordPasses(r ParsedRecord) bool {
	return strings.EqualFold(r.SPFResult, "pass") || strings.EqualFold(r.DKIMResult, "pass")
}

// maxAggregateXML bounds the decompressed payload, so a small archive cannot
// expand into an unbounded allocation.
const maxAggregateXML = 8 << 20

// decompress unwraps the container the reporter used. Both are seen in the
// wild: most senders gzip the XML, some (notably several Microsoft and Yahoo
// endpoints) ship it as a single-entry zip. (A sighting: where and when not
// recorded. RFC 7489 § 7.2.1.1 names gzip only.)
func decompress(raw []byte) ([]byte, error) {
	switch {
	case len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b:
		return gunzip(raw)
	case len(raw) >= 4 && raw[0] == 'P' && raw[1] == 'K' && raw[2] == 0x03 && raw[3] == 0x04:
		return unzip(raw)
	}
	return raw, nil
}

func gunzip(raw []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("dmarc gzip: %w", err)
	}
	defer zr.Close()
	data, err := readAtMost(zr, maxAggregateXML)
	if errors.Is(err, errTooLarge) {
		return nil, fmt.Errorf("dmarc gzip: the report is larger than %d MiB uncompressed", maxAggregateXML>>20)
	}
	if err != nil {
		return nil, fmt.Errorf("dmarc gzip read: %w", err)
	}
	return data, nil
}

// unzip returns the first .xml entry of a zip archive. Directories and any
// other members are skipped rather than trusted.
func unzip(raw []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("dmarc zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("dmarc zip open %q: %w", field(f.Name), err)
		}
		data, err := readAtMost(rc, maxAggregateXML)
		rc.Close()
		if errors.Is(err, errTooLarge) {
			return nil, fmt.Errorf("dmarc zip: the report is larger than %d MiB uncompressed", maxAggregateXML>>20)
		}
		if err != nil {
			return nil, fmt.Errorf("dmarc zip read %q: %w", field(f.Name), err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("dmarc zip: no xml entry")
}

// TightenPolicyHint summarises whether raising p= looks reasonable.
func TightenPolicyHint(pass, fail int, sources []SourceHint) string {
	total := pass + fail
	if total == 0 {
		return "No messages in the reporting window yet."
	}
	failPct := float64(fail) * 100 / float64(total)
	if fail > 0 {
		for _, s := range sources {
			if s.FailCount > 0 && !s.ThisRelay {
				return "Failures come from a source that is not this relay — review before tightening p=."
			}
		}
	}
	if failPct <= 2 && pass > 0 {
		return "Alignment looks strong. Tightening p= may be reasonable."
	}
	if fail > 0 {
		return "Some failures remain. Review sources before tightening p=."
	}
	return "Alignment looks clean for this window."
}

// SourceHint is a panel-facing rollup row with relay detection.
type SourceHint struct {
	SourceIP  string
	PassCount int
	FailCount int
	ThisRelay bool
}
