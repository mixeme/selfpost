package dmarc

import (
	"bytes"
	"encoding/base64"
	"errors"
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

// ErrRejected marks a message that is not a report this relay keeps and never
// will be: it cannot be read as an aggregate report, or it reports on a domain
// that is not sent from here, or it came to another domain's address. Sending
// it again changes nothing, so the caller drops it. Any other error is the
// store's and worth another try.
var ErrRejected = errors.New("dmarc report rejected")

func rejected(err error) error { return fmt.Errorf("%w: %w", ErrRejected, err) }

// MaxMessage is the largest report message taken. One is refused, with that
// reason, rather than cut at the limit and refused as damaged - before
// 2.5.4 a cut message read as a broken attachment (audit 2026-10-12, stage
// 4). Postfix hands over no more than its message_size_limit anyway.
const MaxMessage = 10 << 20

// errTooLarge is a read past its limit.
var errTooLarge = errors.New("larger than its limit")

// readAtMost reads all of r, or fails with errTooLarge once it holds more
// than limit bytes.
func readAtMost(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

// IngestMessage parses a raw RFC 5322 message from r and stores any DMARC
// aggregate attachment it finds.
//
// The report XML is unauthenticated input: anyone who can reach an allow-listed
// rua= address can post one, and the address form is predictable. So the domain
// the report claims is checked twice before anything is stored — it must be a
// domain this relay actually sends for, and, when the report arrived at a
// per-domain hosted address (dmarc-reports+<domain>@<hostname>), it must be the
// domain that address is tagged for. Without this a stranger could file reports
// under someone else's domain, skew the panel's alignment figures, and push
// genuine reports out through the retention cap.
func IngestMessage(st *store.Store, r io.Reader, recipient string, receivedAt time.Time) error {
	raw, err := readAtMost(r, MaxMessage)
	if errors.Is(err, errTooLarge) {
		return rejected(fmt.Errorf("the message is larger than %d MiB", MaxMessage>>20))
	}
	if err != nil {
		return fmt.Errorf("read message: %w", err)
	}
	payload, err := extractAggregatePayload(raw)
	if err != nil {
		return rejected(err)
	}
	parsed, err := ParseAggregate(payload)
	if err != nil {
		return rejected(err)
	}
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if err := checkReportDomain(st, parsed.Domain, recipient); err != nil {
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
		Recipient:    recipient,
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
	return st.PruneDMARCReports(rep.Domain)
}

// checkReportDomain refuses a report whose policy domain is not one of this
// relay's sending domains, or that contradicts the +tag of the address it was
// delivered to.
func checkReportDomain(st *store.Store, domain, recipient string) error {
	if domain == "" {
		return rejected(fmt.Errorf("no policy domain"))
	}
	if tagged := TaggedDomain(recipient); tagged != "" && tagged != domain {
		return rejected(fmt.Errorf("domain %q does not match the address it was sent to (%s)", domain, recipient))
	}
	ok, err := st.DomainExists(domain)
	if err != nil {
		return err
	}
	if !ok {
		return rejected(fmt.Errorf("%q is not a domain configured on this relay", domain))
	}
	return nil
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

// looksLikeAggregate takes a part by its file name or Content-Type. RFC 7489
// § 7.2.1.1: the report is "application/gzip" if compressed and "text/xml"
// otherwise, its name ending ".xml" or ".xml.gz"; zip is outside the RFC and
// is what some reporters send (see decompress).
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
