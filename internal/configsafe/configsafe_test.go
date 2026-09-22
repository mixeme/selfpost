package configsafe

import "testing"

// The check is the last line of defence before a value reaches a config file,
// so it must reject both halves of the problem: an empty value that would write
// a malformed line, and any character from the caller's forbidden set.
func TestToken(t *testing.T) {
	if err := Token("domain", "example.com", " \t\r\n,\\"); err != nil {
		t.Fatalf("clean value rejected: %v", err)
	}
	if err := Token("domain", "", " "); err == nil {
		t.Fatal("empty value accepted")
	}
	for _, bad := range []string{"a b", "a\tb", "a\rb", "a\nb", "a,b", "a\\b"} {
		if err := Token("domain", bad, " \t\r\n,\\"); err == nil {
			t.Errorf("Token(%q) = nil, want error", bad)
		}
	}
	// A character outside the caller's set is that format's business, not this
	// function's: a colon is fatal in an OpenDKIM table and fine in a texthash.
	if err := Token("domain", "a:b", " \t\r\n,\\"); err != nil {
		t.Errorf("character outside the forbidden set rejected: %v", err)
	}
}
