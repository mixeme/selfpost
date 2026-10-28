package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for s, ok := range map[string]bool{
		"2.16.11": true, "0.0.0": true, "10.0.1": true,
		"2.16.11-amd64": false, "v2.16.11": false, "2.16": false, "2.16.011": false,
		"dev": false, "e2e": false, "": false, "2.-1.0": false,
	} {
		if _, got := Parse(s); got != ok {
			t.Errorf("Parse(%q) ok = %v, want %v", s, got, ok)
		}
	}
}

func TestNewestSkipsArchitectureTags(t *testing.T) {
	tags := []string{"1.0.0-arm64", "1.0.0", "2.16.10", "2.16.11-amd64", "2.16.9-amd64", "2.16.2", "sha-abc", "latest"}
	if got := Newest(tags); got != "2.16.10" {
		t.Fatalf("Newest = %q, want 2.16.10 (2.16.11 has only its amd64 tag)", got)
	}
	if got := Newest([]string{"latest"}); got != "" {
		t.Fatalf("Newest without a version = %q", got)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		run, pub string
		want     bool
	}{
		{"2.16.11", "2.16.12", true},
		{"2.16.11", "2.17.0", true},
		{"2.16.11", "2.16.11", false},
		{"2.16.11", "2.16.10", false},
		{"2.16.11", "", false},
		{"dev", "2.16.12", false},
		{"e2e", "2.16.12", false},
	} {
		if got := (State{Running: c.run, Published: c.pub}).Newer(); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.run, c.pub, got)
		}
	}
}

// registry is ghcr.io's two answers: an anonymous token, then the tag list in
// two pages joined by a Link header. It records what was asked.
func registry(t *testing.T, fail bool) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(context.Background()))
		if fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"token":"t0k"}`))
		case r.URL.Path == "/v2/"+Repository+"/tags/list" && r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `</v2/`+Repository+`/tags/list?n=1000&last=2.16.10>; rel="next"`)
			_, _ = w.Write([]byte(`{"name":"` + Repository + `","tags":["1.0.0","2.16.10","2.16.10-amd64"]}`))
		case r.URL.Path == "/v2/"+Repository+"/tags/list":
			_, _ = w.Write([]byte(`{"name":"` + Repository + `","tags":["2.16.12-arm64","2.16.12"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestCheckReadsEveryPageAnonymously(t *testing.T) {
	srv, seen := registry(t, false)
	c := New("2.16.11")
	c.registry = srv.URL
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := c.State("2.16.11")
	if s.Published != "2.16.12" || !s.Newer() || s.Checked.IsZero() || s.Err != "" {
		t.Fatalf("state = %+v", s)
	}
	if len(*seen) != 3 {
		t.Fatalf("%d requests, want token and two pages", len(*seen))
	}
	if q := (*seen)[0].URL.Query().Get("scope"); q != "repository:"+Repository+":pull" {
		t.Errorf("token scope = %q", q)
	}
	for i, r := range *seen {
		if i > 0 && r.Header.Get("Authorization") != "Bearer t0k" {
			t.Errorf("request %d: Authorization = %q", i, r.Header.Get("Authorization"))
		}
		// Nothing about the installation goes out: no version, no host.
		if r.URL.RawQuery != "" && (strings.Contains(r.URL.RawQuery, "2.16.11") || r.Header.Get("Cookie") != "") {
			t.Errorf("request %d carries the installation: %s", i, r.URL)
		}
	}
}

func TestFailureKeepsTheLastAnswer(t *testing.T) {
	srv, _ := registry(t, false)
	c := New("2.16.11")
	c.registry = srv.URL
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	down, _ := registry(t, true)
	c.registry = down.URL
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("a registry that answers 503 is not a check")
	}
	s := c.State("2.16.11")
	if s.Published != "2.16.12" || s.Err == "" {
		t.Fatalf("after a failure: %+v", s)
	}
}

func TestNextPageStaysOnTheRegistry(t *testing.T) {
	if got := nextPage("https://ghcr.io", `<https://elsewhere.example/v2/x?last=1>; rel="next"`); got != "" {
		t.Fatalf("followed a link off the registry: %q", got)
	}
	if got := nextPage("https://ghcr.io", `</v2/x/tags/list?last=1>; rel="next"`); got != "https://ghcr.io/v2/x/tags/list?last=1" {
		t.Fatalf("relative link = %q", got)
	}
	if got := nextPage("https://ghcr.io", ""); got != "" {
		t.Fatalf("no link = %q", got)
	}
}

func TestNilCheckerIsOff(t *testing.T) {
	var c *Checker
	if s := c.State("2.16.11"); !s.Off || s.Running != "2.16.11" || s.Newer() {
		t.Fatalf("nil checker state = %+v", s)
	}
}
