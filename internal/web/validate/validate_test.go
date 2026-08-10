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
