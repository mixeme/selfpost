package validate

import (
	"strings"
	"testing"
)

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

// The minimum counts characters, not bytes: six Cyrillic letters are twelve
// bytes and six characters.
func TestAdminPasswordCountsCharacters(t *testing.T) {
	if err := AdminPassword("пароль"); err == nil {
		t.Error("six Cyrillic letters passed for twelve characters")
	}
	if err := AdminPassword("парольпароль"); err != nil {
		t.Errorf("twelve Cyrillic letters: %v", err)
	}
}

// The maximum counts bytes, not characters: bcrypt hashes at most 72 bytes and
// refuses the rest outright (review 2026-11-05, M1), so the validator names
// the limit before the hash fails as an internal error.
func TestAdminPasswordCeilingCountsBytes(t *testing.T) {
	if err := AdminPassword(strings.Repeat("x", MaxPasswordBytes)); err != nil {
		t.Errorf("%d bytes: %v", MaxPasswordBytes, err)
	}
	for name, p := range map[string]string{
		"seventy-three bytes": strings.Repeat("x", MaxPasswordBytes+1),
		// Thirty-seven Cyrillic letters are seventy-four bytes: over the
		// ceiling, over the floor — the case a non-Latin passphrase hits.
		"thirty-seven cyrillic letters": strings.Repeat("п", 37),
	} {
		err := AdminPassword(p)
		if err == nil {
			t.Errorf("%s passed, want the ceiling", name)
			continue
		}
		if !strings.Contains(err.Error(), "at most") || !strings.Contains(err.Error(), "bytes") {
			t.Errorf("%s: error %q does not name the byte ceiling", name, err)
		}
	}
}

// The backup and export password holds to the same two bounds: the floor in
// characters, the ceiling in bytes — scrypt has no ceiling of its own; the two
// forms read alike (review 2026-11-05, M1).
func TestSecretFilePasswordBounds(t *testing.T) {
	if err := SecretFilePassword(strings.Repeat("x", MinSecretFilePasswordLen)); err != nil {
		t.Errorf("the floor: %v", err)
	}
	if err := SecretFilePassword("пароль"); err == nil {
		t.Error("six Cyrillic letters passed the floor counted in bytes")
	}
	if err := SecretFilePassword(strings.Repeat("x", MaxPasswordBytes+1)); err == nil {
		t.Error("over the ceiling passed")
	}
}
