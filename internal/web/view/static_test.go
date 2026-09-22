package view

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveStatic runs one GET against the static handler.
func serveStatic(path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	StaticHandler().ServeHTTP(rec, r)
	return rec
}

// Every embedded asset must carry a validator. The favicon is the one that
// prompted this: a browser given no ETag and no Last-Modified caches it on a
// guess, and a rebranded panel keeps serving the old mark from the tab.
func TestStaticAssetsCarryETag(t *testing.T) {
	for _, name := range []string{
		"favicon.png", "favicon.svg", "panel.css", "panel.js", "htmx.min.js",
		// The fonts are the assets this matters most for: they are the largest
		// thing the panel serves and the ones a browser is most willing to keep.
		"ibm-plex-sans.woff2", "ibm-plex-mono-400.woff2", "ibm-plex-mono-600.woff2",
		"OFL.txt",
	} {
		rec := serveStatic("/static/"+name, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got status %d, want 200", name, rec.Code)
		}
		if rec.Header().Get("ETag") == "" {
			t.Errorf("%s: no ETag", name)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want %q", name, got, "no-cache")
		}
	}
}

// The point of the ETag is the cheap second request: the browser sends back
// what it has and gets a bodyless 304 when nothing changed.
func TestStaticETagRevalidates(t *testing.T) {
	first := serveStatic("/static/favicon.png", nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}

	same := serveStatic("/static/favicon.png", map[string]string{"If-None-Match": etag})
	if same.Code != http.StatusNotModified {
		t.Errorf("matching If-None-Match: got status %d, want 304", same.Code)
	}
	if same.Body.Len() != 0 {
		t.Errorf("matching If-None-Match: got %d bytes of body, want none", same.Body.Len())
	}

	// A stale validator — what a browser holds after the asset is replaced —
	// has to produce the new bytes rather than another 304.
	stale := serveStatic("/static/favicon.png", map[string]string{"If-None-Match": `"0000000000000000"`})
	if stale.Code != http.StatusOK {
		t.Errorf("stale If-None-Match: got status %d, want 200", stale.Code)
	}
	if stale.Body.Len() == 0 {
		t.Error("stale If-None-Match: empty body, want the asset")
	}
}

// Two different assets must not share a validator, or replacing one would be
// masked by the other's cached copy.
func TestStaticETagsAreContentDerived(t *testing.T) {
	png := serveStatic("/static/favicon.png", nil).Header().Get("ETag")
	svg := serveStatic("/static/favicon.svg", nil).Header().Get("ETag")
	if png == svg {
		t.Errorf("favicon.png and favicon.svg share the ETag %s", png)
	}
}

// OFL condition 2: the licence text must travel with the Font Software. The
// WOFF2 files are embedded; OFL.txt sits next to them so a copy of the panel
// (source tree, image, or /static/OFL.txt) always has it.
func TestOFLTravelsWithFonts(t *testing.T) {
	b, err := fs.ReadFile(assetsFS, "static/OFL.txt")
	if err != nil {
		t.Fatalf("OFL.txt is not embedded next to the Plex WOFF2 files: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, `Reserved Font Name "Plex"`) {
		t.Error("OFL.txt is missing the IBM Plex reserved-font-name notice")
	}
	if !strings.Contains(body, "SIL OPEN FONT LICENSE Version 1.1") {
		t.Error("OFL.txt is missing the SIL OFL 1.1 text")
	}
	rec := serveStatic("/static/OFL.txt", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/OFL.txt: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SIL OPEN FONT LICENSE Version 1.1") {
		t.Error("GET /static/OFL.txt did not serve the OFL text")
	}
}
