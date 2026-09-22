package dmarc

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// IngestMessage parses a raw RFC 5322 message from r and stores any DMARC
// aggregate attachment it finds.
func IngestMessage(st *store.Store, r io.Reader, recipient string, receivedAt time.Time) error {
	raw, err := io.ReadAll(io.LimitReader(r, 12<<20))
	if err != nil {
		return fmt.Errorf("read message: %w", err)
	}
	payload, err := extractAggregatePayload(raw)
	if err != nil {
		return err
	}
	parsed, err := ParseAggregate(payload)
	if err != nil {
		return err
	}
	rep := store.DMARCReport{
		Domain:       parsed.Domain,
		Reporter:     parsed.Reporter,
		ReportID:     parsed.ReportID,
		PeriodBegin:  parsed.PeriodBegin,
		PeriodEnd:    parsed.PeriodEnd,
		ReceivedAt:   receivedAt.UTC(),
		ContactEmail: parsed.ContactEmail,
		PolicyP:      parsed.PolicyP,
		PolicySP:     parsed.PolicySP,
		PolicyPct:    parsed.PolicyPct,
		PolicyADKIM:  parsed.PolicyADKIM,
		PolicyASPF:   parsed.PolicyASPF,
		PassCount:    parsed.PassCount,
		FailCount:    parsed.FailCount,
		Recipient:    strings.ToLower(strings.TrimSpace(recipient)),
	}
	for _, rec := range parsed.Records {
		rep.Records = append(rep.Records, store.DMARCReportRecord{
			SourceIP:    rec.SourceIP,
			Count:       rec.Count,
			Disposition: rec.Disposition,
			SPFResult:   rec.SPFResult,
			DKIMResult:  rec.DKIMResult,
			HeaderFrom:  rec.HeaderFrom,
		})
	}
	if _, err := st.InsertDMARCReport(rep); err != nil {
		return err
	}
	return st.PruneDMARCReports()
}

func extractAggregatePayload(raw []byte) ([]byte, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	ct := msg.Header.Get("Content-Type")
	if ct == "" {
		return io.ReadAll(io.LimitReader(msg.Body, 8<<20))
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, fmt.Errorf("content-type: %w", err)
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil, fmt.Errorf("multipart without boundary")
		}
		mr := multipart.NewReader(msg.Body, boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("multipart: %w", err)
			}
			name := strings.ToLower(part.FileName())
			if name == "" {
				name = strings.ToLower(part.Header.Get("Content-Type"))
			}
			if !looksLikeAggregate(name) {
				continue
			}
			data, err := readEncodedBody(textproto.MIMEHeader(part.Header), part)
			if err != nil {
				return nil, err
			}
			if len(data) > 0 {
				return data, nil
			}
		}
		return nil, fmt.Errorf("no aggregate attachment found")
	}
	return readEncodedBody(textproto.MIMEHeader(msg.Header), msg.Body)
}

func looksLikeAggregate(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "xml") || strings.Contains(name, "gzip") || strings.Contains(name, "zip")
}

func readEncodedBody(hdr textproto.MIMEHeader, r io.Reader) ([]byte, error) {
	encoding := strings.ToLower(hdr.Get("Content-Transfer-Encoding"))
	var body io.Reader = r
	switch encoding {
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		body = quotedprintable.NewReader(r)
	}
	data, err := io.ReadAll(io.LimitReader(body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read part body: %w", err)
	}
	return data, nil
}
