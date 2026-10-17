package handlers

import (
	"testing"
	"time"
)

// A report's window is its reporter's day or days, in UTC, whatever the
// panel's zone; a window that ends at midnight ends the day before.
func TestDMARCWindowIsTheReportersDay(t *testing.T) {
	inZone(t, "Europe/Moscow")
	day := func(d, h, m, s int) time.Time { return time.Date(2026, 9, d, h, m, s, 0, time.UTC) }
	for _, c := range []struct {
		begin, end time.Time
		want       string
	}{
		{day(20, 0, 0, 0), day(21, 0, 0, 0), "20 Sep"},
		{day(20, 0, 0, 0), day(20, 23, 59, 59), "20 Sep"},
		{day(20, 0, 0, 0), day(22, 0, 0, 0), "20 Sep – 21 Sep"},
		{day(20, 12, 0, 0), day(21, 12, 0, 0), "20 Sep – 21 Sep"},
	} {
		if got := formatDMARCWindow(c.begin, c.end); got != c.want {
			t.Errorf("%s – %s = %q, want %q", c.begin, c.end, got, c.want)
		}
	}
}
