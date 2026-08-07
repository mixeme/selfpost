package mailhdr

import (
	"strings"
	"testing"
)

func TestDecodeSubject(t *testing.T) {
	long := strings.Repeat("я", SubjectMaxRunes+10)

	for _, tc := range []struct {
		name, raw, want string
	}{
		{"plain", "Hello there", "Hello there"},
		{"utf8 q", "=?utf-8?Q?=D0=9F=D1=80=D0=BE=D0=B2=D0=B5=D1=80=D0=BA=D0=B0?=", "Проверка"},
		{"utf8 b, folded across two words", "=?utf-8?B?0J/RgNC40LLQtdGC?=\r\n =?utf-8?B?INC80LjRgA==?=", "Привет мир"},
		// No decoder for the legacy single-byte charsets: keep the header as
		// sent rather than losing the subject entirely.
		{"unknown charset", "=?windows-1251?B?z/Do4uXy?=", "=?windows-1251?B?z/Do4uXy?="},
		{"too long", long, strings.Repeat("я", SubjectMaxRunes) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecodeSubject(tc.raw); got != tc.want {
				t.Fatalf("DecodeSubject(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// The panel decodes on the way out as well as the milter on the way in, so
// every already-decoded row in the send log passes through a second time.
func TestDecodeSubjectIsIdempotent(t *testing.T) {
	for _, s := range []string{"Проверка", "Hello there", "", "=?windows-1251?B?z/Do4uXy?="} {
		if got := DecodeSubject(DecodeSubject(s)); got != DecodeSubject(s) {
			t.Fatalf("second pass over %q changed it to %q", s, got)
		}
	}
}
