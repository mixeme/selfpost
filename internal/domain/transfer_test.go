package domain

import (
	"errors"
	"fmt"
	"testing"

	"codeberg.org/mix/selfpost/internal/store"
)

// fakeApps stands in for *app.Service in the domain-transfer tests: it records
// the import calls and hands back canned SASL secrets.
type fakeApps struct {
	secrets   map[string]string
	imported  []importedApp
	importErr error
}

type importedApp struct {
	domainID  int64
	login     string
	mode      string
	addresses []string
	password  string
}

func (f *fakeApps) PurgeDomainSASL(int64) error { return nil }
func (f *fakeApps) Resync() error               { return nil }

func (f *fakeApps) Secret(login string) (string, error) {
	pw, ok := f.secrets[login]
	if !ok {
		return "", fmt.Errorf("no secret for %q", login)
	}
	return pw, nil
}

func (f *fakeApps) ImportApplication(domainID int64, login, mode string, addresses []string, password string) error {
	if f.importErr != nil {
		return f.importErr
	}
	f.imported = append(f.imported, importedApp{domainID, login, mode, addresses, password})
	return nil
}

// newTestService builds a Service over a fresh SQLite store and OpenDKIM tree in
// a temp dir, with the OpenDKIM reload signal stubbed out.
func newTestService(t *testing.T, apps Applications) (*Service, *OpenDKIM) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/selfpost.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	odk := NewOpenDKIM(t.TempDir())
	odk.reload = func() error { return nil }
	return NewService(st, odk, apps, "selfpost"), odk
}

func TestExportImportRoundTrip(t *testing.T) {
	// Source instance: a domain with two applications and their secrets.
	srcApps := &fakeApps{secrets: map[string]string{"mailer": "pw-mailer", "alerts": "pw-alerts"}}
	src, srcOdk := newTestService(t, srcApps)

	d, err := src.Add("example.com")
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if _, err := src.store.AddApplication(d.ID, "mailer", store.AddressModeWildcard, nil); err != nil {
		t.Fatalf("add mailer: %v", err)
	}
	if _, err := src.store.AddApplication(d.ID, "alerts", store.AddressModeList, []string{"a@example.com"}); err != nil {
		t.Fatalf("add alerts: %v", err)
	}

	exp, err := src.Export(d.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if exp.Format != FormatDomainExport || exp.Domain != "example.com" || exp.DKIMSelector != "selfpost" {
		t.Fatalf("export header = %+v", exp)
	}
	if len(exp.Applications) != 2 {
		t.Fatalf("exported %d apps, want 2", len(exp.Applications))
	}
	srcKey, err := srcOdk.ExportKey("example.com", "selfpost")
	if err != nil {
		t.Fatalf("read source key: %v", err)
	}
	if exp.DKIMPrivateKey != string(srcKey) {
		t.Error("export DKIM key does not match the on-disk key")
	}

	// Target instance: import the file.
	dstApps := &fakeApps{}
	dst, dstOdk := newTestService(t, dstApps)
	nd, err := dst.Import(exp)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Domain row landed with the exported selector.
	got, err := dst.Get(nd.ID)
	if err != nil {
		t.Fatalf("get imported domain: %v", err)
	}
	if got.Name != "example.com" || got.DKIMSelector != "selfpost" {
		t.Errorf("imported domain = %+v", got)
	}
	// The DKIM key was imported byte-for-byte, so the DNS record is unchanged.
	dstKey, err := dstOdk.ExportKey("example.com", "selfpost")
	if err != nil {
		t.Fatalf("read imported key: %v", err)
	}
	if string(dstKey) != string(srcKey) {
		t.Error("imported DKIM key differs from the source key")
	}
	// Applications were re-created with their working passwords.
	if len(dstApps.imported) != 2 {
		t.Fatalf("imported %d apps, want 2", len(dstApps.imported))
	}
	byLogin := map[string]importedApp{}
	for _, a := range dstApps.imported {
		byLogin[a.login] = a
	}
	if byLogin["mailer"].password != "pw-mailer" || byLogin["alerts"].password != "pw-alerts" {
		t.Errorf("imported passwords = %+v", dstApps.imported)
	}
	if byLogin["alerts"].mode != store.AddressModeList {
		t.Errorf("alerts mode = %q", byLogin["alerts"].mode)
	}
}

func TestImportRejectsWrongFormat(t *testing.T) {
	dst, _ := newTestService(t, &fakeApps{})
	if _, err := dst.Import(DomainExport{Format: "nope", Domain: "example.com"}); err == nil {
		t.Error("Import accepted a non-export file")
	}
}

func TestImportRejectsDuplicateDomain(t *testing.T) {
	dst, _ := newTestService(t, &fakeApps{})
	if _, err := dst.Add("example.com"); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	// A minimal but well-formed export of the same domain.
	src, srcOdk := newTestService(t, &fakeApps{})
	d, _ := src.Add("example.com")
	key, _ := srcOdk.ExportKey("example.com", "selfpost")
	exp := DomainExport{
		Format: FormatDomainExport, Domain: "example.com", DKIMSelector: "selfpost",
		DKIMPrivateKey: string(key),
	}
	_ = d
	if _, err := dst.Import(exp); !errors.Is(err, store.ErrDomainExists) {
		t.Errorf("Import duplicate = %v, want ErrDomainExists", err)
	}
}

func TestImportRollsBackOnAppFailure(t *testing.T) {
	// Build a valid export from a source instance.
	src, _ := newTestService(t, &fakeApps{secrets: map[string]string{"mailer": "pw"}})
	d, _ := src.Add("example.com")
	if _, err := src.store.AddApplication(d.ID, "mailer", store.AddressModeWildcard, nil); err != nil {
		t.Fatalf("add app: %v", err)
	}
	exp, err := src.Export(d.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Target rejects the application import; the whole domain must roll back.
	dstApps := &fakeApps{importErr: errors.New("boom")}
	dst, dstOdk := newTestService(t, dstApps)
	if _, err := dst.Import(exp); err == nil {
		t.Fatal("Import succeeded despite an application failure")
	}
	// Domain row removed.
	domains, err := dst.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(domains) != 0 {
		t.Errorf("expected rollback to remove the domain, got %+v", domains)
	}
	// DKIM key removed.
	if _, err := dstOdk.ExportKey("example.com", "selfpost"); err == nil {
		t.Error("expected rollback to remove the imported DKIM key")
	}
}
