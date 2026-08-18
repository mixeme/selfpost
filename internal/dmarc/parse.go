package dmarc

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
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
		Domain string `xml:"domain"`
		P      string `xml:"p"`
		SP     string `xml:"sp"`
		Pct    int    `xml:"pct"`
		ADKIM  string `xml:"adkim"`
		ASPF   string `xml:"aspf"`
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

// ParseAggregate decodes gzip-compressed or raw DMARC aggregate XML.
func ParseAggregate(raw []byte) (ParsedReport, error) {
	data, err := maybeGunzip(raw)
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
	out := ParsedReport{
		Reporter:     strings.TrimSpace(doc.Metadata.OrgName),
		ReportID:     strings.TrimSpace(doc.Metadata.ReportID),
		ContactEmail: strings.TrimSpace(doc.Metadata.Email),
		PeriodBegin:  time.Unix(doc.Metadata.DateRange.Begin, 0).UTC(),
		PeriodEnd:    time.Unix(doc.Metadata.DateRange.End, 0).UTC(),
		Domain:       strings.ToLower(strings.TrimSpace(doc.Policy.Domain)),
		PolicyP:      strings.TrimSpace(doc.Policy.P),
		PolicySP:     strings.TrimSpace(doc.Policy.SP),
		PolicyPct:    doc.Policy.Pct,
		PolicyADKIM:  strings.TrimSpace(doc.Policy.ADKIM),
		PolicyASPF:   strings.TrimSpace(doc.Policy.ASPF),
	}
	if out.PolicyPct == 0 {
		out.PolicyPct = 100
	}
	for _, rec := range doc.Records {
		row := ParsedRecord{
			SourceIP:    strings.TrimSpace(rec.Row.SourceIP),
			Count:       rec.Row.Count,
			Disposition: strings.TrimSpace(rec.Row.Policy.Disposition),
			SPFResult:   strings.TrimSpace(rec.Row.Policy.SPF),
			DKIMResult:  strings.TrimSpace(rec.Row.Policy.DKIM),
			HeaderFrom:  strings.TrimSpace(rec.Identifiers.HeaderFrom),
		}
		if row.Count <= 0 {
			row.Count = 1
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

func dmarcRecordPasses(r ParsedRecord) bool {
	return strings.EqualFold(r.SPFResult, "pass") || strings.EqualFold(r.DKIMResult, "pass")
}

func maybeGunzip(raw []byte) ([]byte, error) {
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("dmarc gzip: %w", err)
		}
		defer zr.Close()
		data, err := io.ReadAll(io.LimitReader(zr, 8<<20))
		if err != nil {
			return nil, fmt.Errorf("dmarc gzip read: %w", err)
		}
		return data, nil
	}
	return raw, nil
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
				return "A third-party source is not aligned. Do not tighten p= until that sender is fixed or removed."
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
