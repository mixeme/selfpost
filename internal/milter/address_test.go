package milter

import "testing"

// The journal row's recipient has to be the address Postfix later logs in
// to=<…>, or the delivery line matches no row and the sweep closes it as
// bounced. A quoted local part may hold a space or a ">".
func TestCleanAddress(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<a@example.net>", "a@example.net"},
		{"<a@example.net> SIZE=100 BODY=8BITMIME", "a@example.net"},
		{"a@example.net", "a@example.net"},
		{"a@example.net SIZE=100", "a@example.net"},
		{"<>", ""},
		{`<"a b"@example.net>`, `"a b"@example.net`},
		{`<"a>b"@example.net> NOTIFY=SUCCESS`, `"a>b"@example.net`},
		{`<"a\"> b"@example.net>`, `"a\"> b"@example.net`},
	} {
		if got := cleanAddress(c.in); got != c.want {
			t.Errorf("cleanAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
