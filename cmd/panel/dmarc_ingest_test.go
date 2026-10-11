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

func TestLogSafe(t *testing.T) {
	got := logSafe("zip entry \"a\nstatus=bounced\tx\" \x1b[31m é")
	if want := "zip entry \"a?status=bounced?x\" ?[31m ?"; got != want {
		t.Errorf("logSafe = %q, want %q", got, want)
	}
	long := make([]byte, 1000)
	for i := range long {
		long[i] = 'a'
	}
	if got := logSafe(string(long)); len(got) != 200 {
		t.Errorf("logSafe kept %d bytes of 1000, want 200", len(got))
	}
}
