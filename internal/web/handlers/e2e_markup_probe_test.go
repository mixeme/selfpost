package handlers

import (
	"regexp"
	"strings"
	"testing"
)

// TestE2ESendLogStatusMarkupDrift renders the real deliveries_rows fragment and
// checks whether the e2e gate's HTML scrapers still match it.
func TestE2ESendLogStatusMarkupDrift(t *testing.T) {
	h, _ := serverWithDelivery(t)
	rendered := getBody(t, h.HandleDeliveriesRows, "/deliveries/rows")

	// These mirror test/e2e/main_test.go — keep in sync when fixing the e2e gate.
	statusCellPattern := regexp.MustCompile(`class="st st-[^"]+">(queued|sent|deferred|bounced|rejected)</span>`)
	containsCell := func(html, needle string) bool {
		return strings.Contains(html, `<span class="st st-`) && strings.Contains(html, `">`+needle+`</span>`)
	}

	if statusCellPattern.FindStringSubmatch(rendered) == nil {
		t.Fatalf("e2e statusCellPattern does not match rendered send-log rows:\n%s", snippet(rendered, `class="status"`))
	}
	if !containsCell(rendered, "sent") {
		t.Fatalf("e2e containsCell does not match rendered send-log rows:\n%s", snippet(rendered, `class="status"`))
	}
}

func snippet(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		if len(s) > 200 {
			return s[:200] + "..."
		}
		return s
	}
	start := i - 20
	if start < 0 {
		start = 0
	}
	end := i + 120
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}
