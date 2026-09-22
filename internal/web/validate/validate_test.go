package validate

import "testing"

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"  Example.COM ":   "example.com",
		"MAIL.Example.Org": "mail.example.org",
		"example.com":      "example.com",
	}
	for in, want := range cases {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateDomainValid(t *testing.T) {
	valid := []string{
		"example.com",
		"mail.example.com",
		"a.co",
		"sub-domain.example.co.uk",
		"x1.y2.z3",
		"1example.com",
	}
	for _, d := range valid {
		if err := Domain(d); err != nil {
			t.Errorf("Domain(%q) unexpected error: %v", d, err)
		}
	}
}

func TestValidateDomainInvalid(t *testing.T) {
	invalid := []string{
		"",
		"localhost",
		"example",
		".example.com",
		"example.com.",
		"exa mple.com",
		"example..com",
		"-example.com",
		"example-.com",
		"example.com\n",
		"exa*mple.com",
		"exa_mple.com",
		"Example.com",
		"пример.рф",
		"example.c/m",
	}
	for _, d := range invalid {
		if err := Domain(d); err == nil {
			t.Errorf("Domain(%q) = nil, want error", d)
		}
	}
}

func TestValidateDomainLongLabelRejected(t *testing.T) {
	label := make([]byte, 64)
	for i := range label {
		label[i] = 'a'
	}
	if err := Domain(string(label) + ".com"); err == nil {
		t.Error("expected error for over-long label")
	}
}

func TestHostValid(t *testing.T) {
	valid := []string{
		"10.0.0.8",
		"192.0.2.20",
		"2001:db8::1",
		"mail.internal.example",
		"mx1",
		"mail-1.lan",
	}
	for _, h := range valid {
		if err := Host(h); err != nil {
			t.Errorf("Host(%q) unexpected error: %v", h, err)
		}
	}
}

func TestHostInvalid(t *testing.T) {
	invalid := []string{
		"",
		"exa mple",
		"host/name",
		"host;rm",
		"-bad",
		"bad-",
		"host\nname",
	}
	for _, h := range invalid {
		if err := Host(h); err == nil {
			t.Errorf("Host(%q) = nil, want error", h)
		}
	}
}

func TestNormalizeHostStripsIPv6Brackets(t *testing.T) {
	if got := NormalizeHost("  [2001:DB8::1] "); got != "2001:db8::1" {
		t.Errorf("NormalizeHost IPv6 = %q", got)
	}
	if got := NormalizeHost("Mail.Example.COM"); got != "mail.example.com" {
		t.Errorf("NormalizeHost hostname = %q", got)
	}
}

func TestPort(t *testing.T) {
	n, err := Port("25")
	if err != nil || n != 25 {
		t.Fatalf("Port(25) = %d, %v", n, err)
	}
	for _, raw := range []string{"", "0", "65536", "abc", "-1"} {
		if _, err := Port(raw); err == nil {
			t.Errorf("Port(%q) = nil, want error", raw)
		}
	}
}

func TestMailboxInDomain(t *testing.T) {
	if err := MailboxInDomain("staff@lists.example.com", "lists.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := MailboxInDomain("staff@other.com", "lists.example.com"); err == nil {
		t.Fatal("expected domain mismatch error")
	}
	if err := MailboxInDomain("bad addr@lists.example.com", "lists.example.com"); err == nil {
		t.Fatal("expected local-part error")
	}
}
