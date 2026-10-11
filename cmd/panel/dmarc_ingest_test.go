package main

import "testing"

func TestIngestRecipient(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		// As pipe(8) starts it: the symlink name, then ${recipient}.
		{[]string{"dmarc-reports+Example.org@mail.example.org"}, "dmarc-reports+example.org@mail.example.org"},
		{[]string{"-dmarc-ingest", "dmarc-reports+example.org@mail.example.org"}, "dmarc-reports+example.org@mail.example.org"},
		{[]string{"-dmarc-ingest"}, ""},
		{nil, ""},
	} {
		if got := ingestRecipient(tc.args); got != tc.want {
			t.Errorf("ingestRecipient(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
