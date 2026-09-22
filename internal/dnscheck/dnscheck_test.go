package dnscheck

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/health"
)

// fakeResolver serves a fixed zone, so the checks can be driven through every
// branch without touching the network. An absent name resolves to the same
// "not found" DNSError the standard resolver returns for NXDOMAIN.
type fakeResolver struct {
	txt  map[string][]string
	addr map[string][]net.IPAddr
	ptr  map[string][]string
	mx   map[string][]*net.MX

	// fail names that must return a transient failure instead of an answer.
	fail map[string]bool
	// lookups counts every query, for the cache tests.
	lookups int
}

func notFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.lookups++
	if f.fail[name] {
		return nil, &net.DNSError{Err: "server misbehaving", Name: name, IsTemporary: true}
	}
	if v, ok := f.txt[name]; ok {
		return v, nil
	}
	return nil, notFound(name)
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.lookups++
	if v, ok := f.addr[host]; ok {
		return v, nil
	}
	return nil, notFound(host)
}

func (f *fakeResolver) LookupAddr(_ context.Context, addr string) ([]string, error) {
	f.lookups++
	if v, ok := f.ptr[addr]; ok {
		return v, nil
	}
	return nil, notFound(addr)
}

func (f *fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	f.lookups++
	if v, ok := f.mx[name]; ok {
		return v, nil
	}
	return nil, notFound(name)
}

func ipAddrs(ips ...string) []net.IPAddr {
	out := make([]net.IPAddr, 0, len(ips))
	for _, s := range ips {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out
}

func newTestChecker(f *fakeResolver) *Checker {
	return newChecker(f, time.Second, time.Minute, time.Minute)
}

func TestServerPTRMatches(t *testing.T) {
	f := &fakeResolver{
		addr: map[string][]net.IPAddr{"mail.example.com": ipAddrs("203.0.113.10")},
		ptr:  map[string][]string{"203.0.113.10": {"mail.example.com."}},
	}
	srv := newTestChecker(f).Server("mail.example.com", false)
	if srv.PTR.Status != health.StatusOK {
		t.Fatalf("status = %q (%s)", srv.PTR.Status, srv.PTR.Detail)
	}
	if len(srv.IPs) != 1 || srv.IPs[0] != "203.0.113.10" {
		t.Errorf("IPs = %v, want the forward-resolved address for the SPF check", srv.IPs)
	}
}

func TestServerPTRMismatchIsAnError(t *testing.T) {
	f := &fakeResolver{
		addr: map[string][]net.IPAddr{"mail.example.com": ipAddrs("203.0.113.10")},
		ptr:  map[string][]string{"203.0.113.10": {"static-10.provider.net."}},
	}
	srv := newTestChecker(f).Server("mail.example.com", false)
	if srv.PTR.Status != health.StatusError {
		t.Fatalf("status = %q (%s)", srv.PTR.Status, srv.PTR.Detail)
	}
	if len(srv.PTR.Records) != 1 || !strings.Contains(srv.PTR.Records[0], "static-10.provider.net") {
		t.Errorf("records = %v, want the PTR name that was actually found", srv.PTR.Records)
	}
}

func TestServerPTRMissing(t *testing.T) {
	f := &fakeResolver{addr: map[string][]net.IPAddr{"mail.example.com": ipAddrs("203.0.113.10")}}
	srv := newTestChecker(f).Server("mail.example.com", false)
	if srv.PTR.Status != health.StatusError {
		t.Errorf("status = %q (%s)", srv.PTR.Status, srv.PTR.Detail)
	}
}

func TestServerPartialPTRWarns(t *testing.T) {
	f := &fakeResolver{
		addr: map[string][]net.IPAddr{"mail.example.com": ipAddrs("203.0.113.10", "2001:db8::1")},
		ptr:  map[string][]string{"203.0.113.10": {"mail.example.com."}},
	}
	srv := newTestChecker(f).Server("mail.example.com", false)
	if srv.PTR.Status != health.StatusWarn {
		t.Errorf("status = %q (%s)", srv.PTR.Status, srv.PTR.Detail)
	}
}

func TestServerHostnameDoesNotResolve(t *testing.T) {
	srv := newTestChecker(&fakeResolver{}).Server("mail.example.com", false)
	if srv.PTR.Status != health.StatusError {
		t.Errorf("status = %q (%s)", srv.PTR.Status, srv.PTR.Detail)
	}
	if len(srv.IPs) != 0 {
		t.Errorf("IPs = %v, want none", srv.IPs)
	}
}

func TestServerHostnameUnset(t *testing.T) {
	srv := newTestChecker(&fakeResolver{}).Server("", false)
	if srv.PTR.Status != health.StatusUnknown {
		t.Errorf("status = %q, want unknown when SELFPOST_HOSTNAME is unset", srv.PTR.Status)
	}
}

const testDKIMValue = "v=DKIM1; h=sha256; k=rsa; p=MIIBIjANBgkqTESTKEY"

func dkimQuery(records map[string][]string) (*fakeResolver, Query) {
	q := Query{
		Name:         "example.com",
		Selector:     "selfpost",
		ExpectedDKIM: testDKIMValue,
		ServerIPs:    []string{"203.0.113.10"},
	}
	return &fakeResolver{txt: records}, q
}

func TestDKIMPublishedAndMatching(t *testing.T) {
	f, q := dkimQuery(map[string][]string{
		// Published with different spacing and a line break in the base64, as
		// DNS providers and TXT chunking produce.
		"selfpost._domainkey.example.com": {"v=DKIM1;h=sha256;k=rsa;p=MIIBIjANBgkq TESTKEY"},
	})
	got := newTestChecker(f).Domain(q, false)
	if got.DKIM.Status != health.StatusOK {
		t.Errorf("status = %q (%s)", got.DKIM.Status, got.DKIM.Detail)
	}
}

func TestDKIMMissing(t *testing.T) {
	f, q := dkimQuery(nil)
	got := newTestChecker(f).Domain(q, false)
	if got.DKIM.Status != health.StatusError {
		t.Errorf("status = %q (%s)", got.DKIM.Status, got.DKIM.Detail)
	}
}

func TestDKIMWrongKey(t *testing.T) {
	f, q := dkimQuery(map[string][]string{
		"selfpost._domainkey.example.com": {"v=DKIM1; h=sha256; k=rsa; p=SOMEOTHERKEY"},
	})
	got := newTestChecker(f).Domain(q, false)
	if got.DKIM.Status != health.StatusError {
		t.Errorf("status = %q (%s)", got.DKIM.Status, got.DKIM.Detail)
	}
	if !strings.Contains(got.DKIM.Detail, "not the one this server signs with") {
		t.Errorf("detail does not explain the mismatch: %s", got.DKIM.Detail)
	}
}

func TestDKIMRevoked(t *testing.T) {
	f, q := dkimQuery(map[string][]string{
		"selfpost._domainkey.example.com": {"v=DKIM1; h=sha256; k=rsa; p="},
	})
	got := newTestChecker(f).Domain(q, false)
	if got.DKIM.Status != health.StatusError || !strings.Contains(got.DKIM.Detail, "revokes") {
		t.Errorf("status = %q (%s)", got.DKIM.Status, got.DKIM.Detail)
	}
}

func TestDKIMLookupFailureIsUnknown(t *testing.T) {
	f, q := dkimQuery(nil)
	f.fail = map[string]bool{"selfpost._domainkey.example.com": true}
	got := newTestChecker(f).Domain(q, false)
	if got.DKIM.Status != health.StatusUnknown {
		t.Errorf("status = %q (%s), want unknown when the resolver fails", got.DKIM.Status, got.DKIM.Detail)
	}
}

func TestSPF(t *testing.T) {
	cases := []struct {
		name   string
		record []string
		want   health.Status
	}{
		{"literal ip4", []string{"v=spf1 ip4:203.0.113.10 -all"}, health.StatusOK},
		{"covering CIDR", []string{"v=spf1 ip4:203.0.113.0/24 -all"}, health.StatusOK},
		{"other address only", []string{"v=spf1 ip4:198.51.100.7 -all"}, health.StatusError},
		{"include cannot be followed", []string{"v=spf1 include:_spf.provider.net -all"}, health.StatusWarn},
		{"plus all", []string{"v=spf1 +all"}, health.StatusWarn},
		{"negative qualifier does not authorise", []string{"v=spf1 -ip4:203.0.113.10 -all"}, health.StatusError},
		{"two records", []string{"v=spf1 ip4:203.0.113.10 -all", "v=spf1 -all"}, health.StatusError},
		{"no record", nil, health.StatusError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			txt := map[string][]string{}
			if c.record != nil {
				txt["example.com"] = c.record
			}
			f := &fakeResolver{txt: txt}
			got := newTestChecker(f).checkSPF(context.Background(), Query{
				Name:      "example.com",
				ServerIPs: []string{"203.0.113.10"},
			})
			if got.Status != c.want {
				t.Errorf("status = %q, want %q (%s)", got.Status, c.want, got.Detail)
			}
		})
	}
}

func TestSPFAMechanism(t *testing.T) {
	f := &fakeResolver{
		txt:  map[string][]string{"example.com": {"v=spf1 a -all"}},
		addr: map[string][]net.IPAddr{"example.com": ipAddrs("203.0.113.10")},
	}
	got := newTestChecker(f).checkSPF(context.Background(), Query{
		Name:      "example.com",
		ServerIPs: []string{"203.0.113.10"},
	})
	if got.Status != health.StatusOK {
		t.Errorf("status = %q (%s)", got.Status, got.Detail)
	}
}

func TestSPFMXMechanism(t *testing.T) {
	f := &fakeResolver{
		txt:  map[string][]string{"example.com": {"v=spf1 mx -all"}},
		mx:   map[string][]*net.MX{"example.com": {{Host: "mail.example.com.", Pref: 10}}},
		addr: map[string][]net.IPAddr{"mail.example.com": ipAddrs("203.0.113.10")},
	}
	got := newTestChecker(f).checkSPF(context.Background(), Query{
		Name:      "example.com",
		ServerIPs: []string{"203.0.113.10"},
	})
	if got.Status != health.StatusOK {
		t.Errorf("status = %q (%s)", got.Status, got.Detail)
	}
}

func TestSPFWithoutServerIPIsUnknown(t *testing.T) {
	f := &fakeResolver{txt: map[string][]string{"example.com": {"v=spf1 -all"}}}
	got := newTestChecker(f).checkSPF(context.Background(), Query{Name: "example.com"})
	if got.Status != health.StatusUnknown {
		t.Errorf("status = %q (%s)", got.Status, got.Detail)
	}
}

func TestDMARC(t *testing.T) {
	cases := []struct {
		name   string
		record []string
		want   health.Status
	}{
		{"reject", []string{"v=DMARC1; p=reject; rua=mailto:dmarc@example.com"}, health.StatusOK},
		{"none", []string{"v=DMARC1; p=none"}, health.StatusOK},
		{"no policy tag", []string{"v=DMARC1; rua=mailto:dmarc@example.com"}, health.StatusWarn},
		{"absent", nil, health.StatusWarn},
		{"duplicated", []string{"v=DMARC1; p=none", "v=DMARC1; p=reject"}, health.StatusError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			txt := map[string][]string{}
			if c.record != nil {
				txt["_dmarc.example.com"] = c.record
			}
			f := &fakeResolver{txt: txt}
			got := newTestChecker(f).checkDMARC(context.Background(), Query{Name: "example.com"})
			if got.Status != c.want {
				t.Errorf("status = %q, want %q (%s)", got.Status, c.want, got.Detail)
			}
		})
	}
}

func TestDMARCNonePolicyIsExplained(t *testing.T) {
	f := &fakeResolver{txt: map[string][]string{"_dmarc.example.com": {"v=DMARC1; p=none"}}}
	got := newTestChecker(f).checkDMARC(context.Background(), Query{Name: "example.com"})
	if !strings.Contains(got.Detail, "monitoring only") {
		t.Errorf("p=none is not explained: %s", got.Detail)
	}
}

func TestResultsAreCachedAndForceBypassesTheCache(t *testing.T) {
	f := &fakeResolver{
		addr: map[string][]net.IPAddr{"mail.example.com": ipAddrs("203.0.113.10")},
		ptr:  map[string][]string{"203.0.113.10": {"mail.example.com."}},
	}
	c := newTestChecker(f)

	c.Server("mail.example.com", false)
	after := f.lookups
	if after == 0 {
		t.Fatal("the first check did not query the resolver")
	}
	c.Server("mail.example.com", false)
	if f.lookups != after {
		t.Errorf("a second check re-queried DNS: %d lookups, want %d", f.lookups, after)
	}
	c.Server("mail.example.com", true)
	if f.lookups == after {
		t.Error("force did not bypass the cache")
	}
}

func TestDomainOverallIsTheWorstOfTheThree(t *testing.T) {
	f, q := dkimQuery(map[string][]string{
		"selfpost._domainkey.example.com": {testDKIMValue},
		"example.com":                     {"v=spf1 ip4:203.0.113.10 -all"},
		// No DMARC: a warning.
	})
	got := newTestChecker(f).Domain(q, false)
	if got.DKIM.Status != health.StatusOK || got.SPF.Status != health.StatusOK {
		t.Fatalf("DKIM=%q SPF=%q", got.DKIM.Status, got.SPF.Status)
	}
	if got.Overall != health.StatusWarn {
		t.Errorf("overall = %q, want the DMARC warning to surface", got.Overall)
	}
}

func TestForgetDropsTheCachedDomain(t *testing.T) {
	f, q := dkimQuery(map[string][]string{"selfpost._domainkey.example.com": {testDKIMValue}})
	c := newTestChecker(f)
	c.Domain(q, false)
	before := f.lookups
	c.Forget(q.Name)
	c.Domain(q, false)
	if f.lookups == before {
		t.Error("Forget did not drop the cached result")
	}
}

func TestReportAuth(t *testing.T) {
	f := &fakeResolver{txt: map[string][]string{"_report._dmarc.hub.example": {"v=DMARC1;"}}}
	got := newTestChecker(f).checkReportAuth(context.Background(), "hub.example")
	if got.Status != health.StatusOK {
		t.Fatalf("status = %q (%s)", got.Status, got.Detail)
	}

	f = &fakeResolver{}
	got = newTestChecker(f).checkReportAuth(context.Background(), "hub.example")
	if got.Status != health.StatusWarn {
		t.Fatalf("missing = %q, want warn", got.Status)
	}
	if !strings.Contains(got.Detail, ReportAuthExample()) {
		t.Errorf("advice %q should cite %q", got.Detail, ReportAuthExample())
	}
}

func TestInboundMXPointsAtServer(t *testing.T) {
	f := &fakeResolver{
		mx: map[string][]*net.MX{
			"lists.example.com": {
				{Host: "mail.primary.example.net.", Pref: 10},
				{Host: "mail.example.org.", Pref: 20},
			},
		},
	}
	got := newTestChecker(f).InboundMX("lists.example.com", "mail.example.org", false)
	if got.Status != health.StatusOK {
		t.Fatalf("status = %q (%s)", got.Status, got.Detail)
	}
}

func TestInboundMXMissingThisServer(t *testing.T) {
	f := &fakeResolver{
		mx: map[string][]*net.MX{
			"backup.example.net": {{Host: "mail.primary.example.net.", Pref: 10}},
		},
	}
	got := newTestChecker(f).InboundMX("backup.example.net", "mail.example.org", false)
	if got.Status != health.StatusError {
		t.Fatalf("status = %q, want error", got.Status)
	}
}

func TestInboundMXAbsent(t *testing.T) {
	got := newTestChecker(&fakeResolver{}).InboundMX("none.example", "mail.example.org", false)
	if got.Status != health.StatusError {
		t.Fatalf("status = %q, want error", got.Status)
	}
}
