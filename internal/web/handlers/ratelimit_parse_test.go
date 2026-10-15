package handlers

import (
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// An auto limit's multiplier is a number within its bounds: NaN and the
// infinities, which strconv reads, are refused like any other number out of
// range (audit 2026-10-12, L11).
func TestAutoMultiplierIsANumberInRange(t *testing.T) {
	for _, raw := range []string{"NaN", "nan", "+Inf", "-Inf", "inf", "1.4", "5.1", "x"} {
		if v, err := parseAutoMultiplier(raw); err == nil {
			t.Errorf("%q accepted as %v", raw, v)
		}
	}
	for raw, want := range map[string]float64{"": store.DefaultAutoMultiplier, "1.5": 1.5, " 2.5 ": 2.5, "5": 5} {
		if v, err := parseAutoMultiplier(raw); err != nil || v != want {
			t.Errorf("%q = %v, %v; want %v", raw, v, err, want)
		}
	}
}
