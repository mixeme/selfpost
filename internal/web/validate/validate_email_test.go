package validate

import "testing"

func TestValidateEmail(t *testing.T) {
	if err := Email(""); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := Email("reports@mail.example.com"); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := Email("bad"); err == nil {
		t.Error("bad address accepted")
	}
	if err := Email("x@gmail.com"); err == nil {
		t.Error("gmail accepted")
	}
	if err := Email("reports+rua@mail.example.com"); err != nil {
		t.Errorf("plus-address valid: %v", err)
	}
	if err := Email("bad space@mail.example.com"); err == nil {
		t.Error("address with space accepted")
	}
	if err := Email("привет@mail.example.com"); err == nil {
		t.Error("non-ascii local-part accepted")
	}
	if err := Email("x@-example.com"); err == nil {
		t.Error("invalid domain accepted")
	}
	if err := Email("x@protonmail.com"); err == nil {
		t.Error("blocked freemail domain accepted")
	}
}

// A user's own address is where the panel writes to them, and a mailbox at a
// public host is a good place for that - the refusal of those hosts belongs to
// DMARC report addresses alone. What cannot be an address is refused for both,
// and so is one that would read as an option on sendmail's command line.
func TestAccountEmail(t *testing.T) {
	for _, ok := range []string{"", "me@gmail.com", "first.last+tag@proton.me", "admin@example.org"} {
		if err := AccountEmail(ok); err != nil {
			t.Errorf("AccountEmail(%q) = %v, want accepted", ok, err)
		}
	}
	for _, bad := range []string{"no-at-sign", "@example.org", "me@", "a b@example.org", "me@exa mple.org", "-oQ/tmp@example.org", "\u00fc@example.org"} {
		if err := AccountEmail(bad); err == nil {
			t.Errorf("AccountEmail(%q) accepted", bad)
		}
	}
	if err := Email("-x@example.org"); err == nil {
		t.Error("Email accepted an address that begins like an option")
	}
}
