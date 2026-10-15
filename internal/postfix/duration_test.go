package postfix

import (
	"testing"
	"time"
)

// Postfix 3.10.13, src/global/conv_time.c, conv_time(): one number, one
// lower-case unit, the default unit when there is none, the rest ignored.
func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		unit byte
		want time.Duration
	}{
		{"5d", 's', 5 * 24 * time.Hour},
		{"300s", 'd', 300 * time.Second},
		{"4000s", 's', 4000 * time.Second},
		{"1h", 's', time.Hour},
		{"0", 's', 0},
		{"0s", 's', 0},
		{"300", 's', 300 * time.Second},
		{"3", 'd', 3 * 24 * time.Hour}, // maximal_queue_lifetime = 3
		{"2", 'h', 2 * time.Hour},      // delay_warning_time = 2
		{" 300s ", 's', 300 * time.Second},
		{"+5m", 's', 5 * time.Minute},
		{"1h7m", 's', time.Hour}, // what follows the unit is ignored
		{"1h 5x", 's', time.Hour},
		{"1w", 's', 7 * 24 * time.Hour},
		{"2m", 's', 2 * time.Minute},
		{"s", 'd', 0}, // strtol() reads nothing and stops at the "s"
	}
	for _, tc := range cases {
		got, err := ParseTime(tc.in, tc.unit)
		if err != nil {
			t.Errorf("ParseTime(%q, %c): %v", tc.in, tc.unit, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTime(%q, %c) = %v, want %v", tc.in, tc.unit, got, tc.want)
		}
	}
}

func TestParseTimeRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "foo", "5x", "-300s", "5D", "1H", " s", "99999999999s", "400000w"} {
		if _, err := ParseTime(in, 's'); err == nil {
			t.Errorf("ParseTime(%q) = nil, want error", in)
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
