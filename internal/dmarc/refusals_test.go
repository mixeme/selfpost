package dmarc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// Reports arrive by mail from anyone. One that is malformed, too large, or
// compressed past what a report may hold is refused with its reason, and
// nothing is stored (audit 2026-10-12, stage 4; rows of
// review-2026-08-followups).
func TestIngestRefusesWhatCannotBeAReport(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddDomain("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	attach := func(ctype string, payload []byte) string {
		return "From: reporter@example.net\r\nMIME-Version: 1.0\r\nContent-Type: " + ctype +
			"\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString(payload) + "\r\n"
	}
	// A gzip of nine MiB of zeroes is some nine KiB: small to send, past the
	// eight MiB a report may hold once opened.
	var bomb bytes.Buffer
	zw := gzip.NewWriter(&bomb)
	zw.Write(make([]byte, 9<<20))
	zw.Close()
	var zipBomb bytes.Buffer
	zz := zip.NewWriter(&zipBomb)
	f, _ := zz.Create("report.xml")
	f.Write(make([]byte, 9<<20))
	zz.Close()
	var malformed bytes.Buffer
	zw = gzip.NewWriter(&malformed)
	zw.Write([]byte("<feedback><report_metadata><org_name>x</org_name>"))
	zw.Close()

	for _, c := range []struct {
		name string
		msg  string
		want string
	}{
		{"malformed XML", attach("application/gzip", malformed.Bytes()), "xml"},
		{"a gzip bomb", attach("application/gzip", bomb.Bytes()), "larger than 8 MiB uncompressed"},
		{"a zip bomb", attach("application/zip", zipBomb.Bytes()), "larger than 8 MiB uncompressed"},
		{"a message too large", "From: r@example.net\r\n\r\n" + strings.Repeat("x", MaxMessage), "larger than 10 MiB"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := IngestMessage(st, strings.NewReader(c.msg), "dmarc-reports+example.com@mail.example.com", time.Now().UTC())
			if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want a refusal saying %q", err, c.want)
			}
			if reports, err := st.ListDMARCReportsForDomain("example.com", 10); err != nil || len(reports) != 0 {
				t.Errorf("stored %d reports (%v)", len(reports), err)
			}
		})
	}
}

// recordingMaps is Postfix as the service sees it: the maps a resync writes.
type recordingMaps struct{ got []postfix.DMARCMapsConfig }

func (m *recordingMaps) RebuildDMARCMaps(cfg postfix.DMARCMapsConfig) error {
	m.got = append(m.got, cfg)
	return nil
}

// A resync writes the addresses of the domains that report to this host, and
// their domains; switched off, it writes empty maps, so Postfix accepts
// nothing for reports. The hosted address is on this host and named after
// the domain; one on another host is refused.
func TestServiceResyncAndHostedAddress(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, _ := st.AddDomain("example.org", "sel")
	maps := &recordingMaps{}
	s := NewService(st, maps, " Mail.Example.COM ", true)
	if got := s.HostedSuggestion("example.org"); got != "dmarc-reports+example.org@mail.example.com" {
		t.Errorf("suggestion = %q", got)
	}
	if err := st.SetDomainDMARCAddress(d.ID, s.HostedSuggestion("example.org")); err != nil {
		t.Fatal(err)
	}
	if err := s.Resync(); err != nil {
		t.Fatal(err)
	}
	want := postfix.DMARCMapsConfig{Recipients: []string{"dmarc-reports+example.org@mail.example.com"}, Domains: []string{"mail.example.com"}}
	if len(maps.got) != 1 || !reflect.DeepEqual(maps.got[0], want) {
		t.Errorf("maps = %+v, want %+v", maps.got, want)
	}

	off := NewService(st, maps, "mail.example.com", false)
	if err := off.Resync(); err != nil {
		t.Fatal(err)
	}
	if last := maps.got[len(maps.got)-1]; len(last.Recipients) != 0 || len(last.Domains) != 0 {
		t.Errorf("switched off, the maps hold %+v", last)
	}

	if err := s.ValidateHostedAddress("dmarc-reports+example.org@mail.example.com"); err != nil {
		t.Errorf("the hosted address refused: %v", err)
	}
	if err := s.ValidateHostedAddress("reports@elsewhere.example"); err == nil {
		t.Error("an address on another host passed as hosted")
	}
}
