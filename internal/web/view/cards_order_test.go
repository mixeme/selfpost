package view

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// On a phone the cards layout shows its boxes in the order of their numbers,
// whichever stack they stand in: each box of the layout carries its number as
// data-order, and the stylesheet has a rule for it. Before, the stylesheet
// ordered by a box's place in its stack and knew four to a stack; a fifth
// landed out of place with nothing failing (owner, 2026-10-13).
func TestCardsBoxesCarryTheirNumberAsTheirOrder(t *testing.T) {
	css, err := os.ReadFile("static/panel.css")
	if err != nil {
		t.Fatal(err)
	}
	box := regexp.MustCompile(`<div class="box[^"]*"[^>]*?(?: data-order="(\d+)")?>\s*<div class="sp-box-head"><span class="sp-no">(\d+)</span>`)
	rec := httptest.NewRecorder()
	kitEngine(t).Render(rec, http.StatusOK, "components", KitPage())
	for name, page := range map[string]string{
		"health": renderSignedIn(t, "health", healthFixture(false)),
		"kit":    rec.Body.String(),
	} {
		start := strings.Index(page, `<div class="columns sp-cards">`)
		if start < 0 {
			t.Fatalf("%s has no cards layout", name)
		}
		// The layout runs to the first page part after it that is not a box
		// of it; its boxes are the numbered ones up to there.
		region := page[start:]
		if end := strings.Index(region, `<footer`); end > 0 {
			region = region[:end]
		}
		found := 0
		for _, m := range box.FindAllStringSubmatch(region, -1) {
			order, no := m[1], strings.TrimLeft(m[2], "0")
			if name == "kit" && found == 3 {
				break // the kit's cards hold three numbered boxes; the rest of the page is other sections
			}
			found++
			if order != no {
				t.Errorf("%s: box %s carries data-order %q", name, m[2], order)
				continue
			}
			if !strings.Contains(string(css), `.sp-cards [data-order="`+order+`"] { order: `+order+`; }`) {
				t.Errorf("%s: panel.css has no rule for data-order %s", name, order)
			}
		}
		if want := map[string]int{"health": 8, "kit": 3}[name]; found != want {
			t.Errorf("%s: %d numbered boxes found in the cards layout, want %d", name, found, want)
		}
	}
}

// Outside the cards layout a box has no order: the attribute is the layout's.
func TestABoxOutsideCardsHasNoOrder(t *testing.T) {
	if got := (Box{No: "03"}).Order(); got != "" {
		t.Errorf("a box outside cards has order %q", got)
	}
	if got := (Box{No: "03", InCards: true}).Order(); got != "3" {
		t.Errorf("a cards box 03 has order %q", got)
	}
	if got := (Box{Icon: "ti-x", InCards: true}).Order(); got != "" {
		t.Errorf("a cards box without a number has order %q", got)
	}
}
