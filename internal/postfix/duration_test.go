package postfix

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"5d", 5 * 24 * time.Hour},
		{"300s", 300 * time.Second},
		{"4000s", 4000 * time.Second},
		{"1h", time.Hour},
		{"0", 0},
		{"0s", 0},
		{"300", 300 * time.Second},
		{" 300s ", 300 * time.Second},
		{"1h7m", time.Hour + 7*time.Minute},
		{"1w", 7 * 24 * time.Hour},
		{"2m", 2 * time.Minute},
	}
	for _, tc := range cases {
		got, err := ParseDuration(tc.in)
		if err != nil {
			t.Errorf("ParseDuration(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseDurationRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "foo", "5x", "-300s", "s", "1h 5x"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) = nil, want error", in)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{5 * 24 * time.Hour, "5 days"},
		{300 * time.Second, "5 minutes"},
		{4000 * time.Second, "about 1 hour 7 minutes"},
		{time.Hour, "1 hour"},
		{0, "0 seconds"},
		{time.Second, "1 second"},
		{2 * time.Minute, "2 minutes"},
		{24 * time.Hour, "1 day"},
	}
	for _, tc := range cases {
		if got := FormatDuration(tc.in); got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
