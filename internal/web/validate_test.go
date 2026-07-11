package web

import "testing"

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"  Example.COM ":   "example.com",
		"MAIL.Example.Org": "mail.example.org",
		"example.com":      "example.com",
	}
	for in, want := range cases {
		if got := normalizeDomain(in); got != want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", in, got, want)
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
		if err := validateDomain(d); err != nil {
			t.Errorf("validateDomain(%q) unexpected error: %v", d, err)
		}
	}
}

func TestValidateDomainInvalid(t *testing.T) {
	invalid := []string{
		"",              // empty
		"localhost",     // single label
		"example",       // single label
		".example.com",  // leading dot -> empty label
		"example.com.",  // trailing dot -> empty label
		"exa mple.com",  // space
		"example..com",  // empty label
		"-example.com",  // label starts with '-'
		"example-.com",  // label ends with '-'
		"example.com\n", // newline (config injection attempt)
		"exa*mple.com",  // disallowed char
		"exa_mple.com",  // underscore not allowed in domains
		"Example.com",   // upper-case (must be normalised first)
		"пример.рф",     // non-ASCII
		"example.c/m",   // slash (path-traversal attempt)
	}
	for _, d := range invalid {
		if err := validateDomain(d); err == nil {
			t.Errorf("validateDomain(%q) = nil, want error", d)
		}
	}
}

func TestValidateDomainLongLabelRejected(t *testing.T) {
	label := make([]byte, 64)
	for i := range label {
		label[i] = 'a'
	}
	if err := validateDomain(string(label) + ".com"); err == nil {
		t.Error("expected error for over-long label")
	}
}
