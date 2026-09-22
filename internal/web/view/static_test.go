package view

import (
	"net/http"
	"net/http/httptest"
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
