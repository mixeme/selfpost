package app

import "testing"

func TestValidateLogin(t *testing.T) {
	good := []string{"alerts", "prod-server", "app_1", "News.Letter"}
	for _, l := range good {
		if err := validateLogin(l); err != nil {
			t.Errorf("validateLogin(%q) = %v, want nil", l, err)
		}
	}
	bad := []string{
		"ab",                 // too short
		"alerts@example.com", // '@' not allowed (sasldb realm separator)
		"has space",          // whitespace
		"inject\nline",       // newline
		"comma,login",        // map value separator
		"colon:login",        // config separator
	}
	for _, l := range bad {
		if err := validateLogin(l); err == nil {
			t.Errorf("validateLogin(%q) = nil, want error", l)
		}
	}
}

func TestValidateSenderAddressDomainOwnership(t *testing.T) {
	// The critical check (spec 7.6.2): an address must belong to the app's domain.
	if err := validateSenderAddress("alerts@example.com", "example.com"); err != nil {
		t.Errorf("same-domain address rejected: %v", err)
	}
	if err := validateSenderAddress("alerts@evil.com", "example.com"); err == nil {
		t.Error("cross-domain address accepted, want rejection")
	}
	// A trailing-domain trick must not pass as ownership.
	if err := validateSenderAddress("a@notexample.com", "example.com"); err == nil {
		t.Error("suffix-domain address accepted, want rejection")
	}
}

func TestValidateSenderAddressForm(t *testing.T) {
	bad := []string{
		"noat.example.com",    // no '@'
		"@example.com",        // empty local part
		".dot@example.com",    // leading dot
		"dot.@example.com",    // trailing dot
		"in ject@example.com", // space
		"quote\"@example.com", // disallowed char
	}
	for _, a := range bad {
		if err := validateSenderAddress(a, "example.com"); err == nil {
			t.Errorf("validateSenderAddress(%q) = nil, want error", a)
		}
	}
}

func TestParseAddresses(t *testing.T) {
	// Normalises case, trims, drops blanks, de-duplicates.
	got, err := parseAddresses([]string{" Alerts@Example.com ", "", "noreply@example.com", "alerts@example.com"}, "example.com")
	if err != nil {
		t.Fatalf("parseAddresses: %v", err)
	}
	if len(got) != 2 || got[0] != "alerts@example.com" || got[1] != "noreply@example.com" {
		t.Fatalf("parseAddresses = %v", got)
	}

	// Empty list in list mode is an error.
	if _, err := parseAddresses([]string{"", "  "}, "example.com"); err == nil {
		t.Error("empty address list accepted, want error")
	}
	// A cross-domain address rejects the whole submission.
	if _, err := parseAddresses([]string{"ok@example.com", "bad@other.com"}, "example.com"); err == nil {
		t.Error("cross-domain address in list accepted, want error")
	}
}

func TestGeneratePasswordStrength(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		p, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		if len(p) < 30 {
			t.Fatalf("password too short: %d chars", len(p))
		}
		if seen[p] {
			t.Fatalf("duplicate password generated: %q", p)
		}
		seen[p] = true
	}
}
