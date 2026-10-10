package handlers

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

// The inbound spam filter's socket is one of the milter sockets on Health and
// counts in the Milter sockets card of Overview — and only when the filter is on.

// socketsServer is a server whose two Postfix milter sockets answer, so that
// every difference in the Milter sockets card comes from the filter.
func socketsServer(t *testing.T) *Handlers {
	t.Helper()
	h, _ := settingsServer(t)
	h.machine = &health.MachineSampler{}
	dir := t.TempDir()
	h.cfg.OpenDKIMSocket = filepath.Join(dir, "opendkim.sock")
	h.cfg.JournalSocket = filepath.Join(dir, "journal.sock")
	for _, path := range []string{h.cfg.OpenDKIMSocket, h.cfg.JournalSocket} {
		l, err := net.Listen("unix", path)
		if err != nil {
			t.Skipf("unix sockets unavailable here: %v", err)
		}
		t.Cleanup(func() { l.Close() })
	}
	return h
}

func socketsOf(t *testing.T, h *Handlers) []health.Socket {
	t.Helper()
	return h.healthChecks()["Sockets"].([]health.Socket)
}

func socketCard(t *testing.T, h *Handlers) view.HealthCard {
	t.Helper()
	for _, c := range healthCards(h.healthChecks()) {
		if c.Name == "Milter sockets" {
			return c
		}
	}
	t.Fatal("no Milter sockets card")
	return view.HealthCard{}
}

func TestFilterOffAddsNoSocket(t *testing.T) {
	h := socketsServer(t)
	h.cfg.InboundEnabled = true
	h.cfg.InboundAntispamMilter = "" // no filter attached
	if got := socketsOf(t, h); len(got) != 2 {
		t.Fatalf("%d sockets with the filter off, want 2: %+v", len(got), got)
	}

	// A milter address with the inbound relay off is not a filter either.
	h.cfg.InboundEnabled = false
	h.cfg.InboundAntispamMilter = "inet:127.0.0.1:1"
	got := socketsOf(t, h)
	if len(got) != 2 {
		t.Fatalf("%d sockets with the inbound relay off, want 2: %+v", len(got), got)
	}
	card := socketCard(t, h)
	if card.Value != "2 of 2 answering" || card.Sub != "OpenDKIM, send-log" || card.Level != "" {
		t.Errorf("card = %+v", card)
	}
}

func TestFilterOnAnsweringIsTheThirdSocket(t *testing.T) {
	h := socketsServer(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp unavailable here: %v", err)
	}
	defer l.Close()
	h.cfg.InboundEnabled = true
	h.cfg.InboundAntispamMilter, h.cfg.InboundAntispamAction = "inet:"+l.Addr().String(), "tempfail"

	got := socketsOf(t, h)
	if len(got) != 3 || got[2].Name != "Spam filter" || got[2].Note != "inbound" || got[2].Path != "inet:"+l.Addr().String() {
		t.Fatalf("sockets = %+v", got)
	}
	for _, s := range got {
		if !s.Present || s.Status != health.StatusOK {
			t.Errorf("%s: present=%v status=%q (%s)", s.Name, s.Present, s.Status, s.Detail)
		}
	}
	card := socketCard(t, h)
	if card.Value != "3 of 3 answering" || card.Sub != "OpenDKIM, send-log, Spam filter" || card.Level != "" {
		t.Errorf("card = %+v", card)
	}

	out := renderHealth(t, h, h.healthChecks(), "")
	for _, want := range []string{
		`Spam filter <span class="sp-muted">· inbound</span>`, "inet:" + l.Addr().String(),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Health is missing %q", want)
		}
	}
	if strings.Contains(out, "Listening") {
		t.Error("a socket that answers repeats its detail beside the tag")
	}
}

// A filter that does not answer is a socket that is missing: an error when
// Postfix defers inbound mail while it is down (as for OpenDKIM), a warning when
// it accepts the mail unfiltered (as for the send log).
func TestFilterDownIsAMissingSocket(t *testing.T) {
	h := socketsServer(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp unavailable here: %v", err)
	}
	dead := "inet:" + l.Addr().String()
	l.Close()
	h.cfg.InboundEnabled = true

	for _, tt := range []struct {
		action, level, stamp, detail string
		status                       health.Status
	}{
		{"tempfail", "fail", "FAIL", "Inbound mail is deferred until it is back.", health.StatusError},
		{"accept", "warn", "WARN", "Inbound mail goes through unfiltered.", health.StatusWarn},
		{"", "warn", "WARN", "Inbound mail goes through unfiltered.", health.StatusWarn},
	} {
		h.cfg.InboundAntispamMilter, h.cfg.InboundAntispamAction = dead, tt.action
		got := socketsOf(t, h)
		if len(got) != 3 {
			t.Fatalf("action %q: %d sockets, want 3", tt.action, len(got))
		}
		f := got[2]
		refused := "Connection to " + l.Addr().String() + " refused. " + tt.detail
		if f.Present || f.Status != tt.status || f.Detail != refused {
			t.Errorf("action %q: filter = %+v, want status %q, detail %q", tt.action, f, tt.status, refused)
		}

		c := h.healthChecks()
		if c["SocketStatus"].(health.Status) != tt.status {
			t.Errorf("action %q: SocketStatus = %q, want %q", tt.action, c["SocketStatus"], tt.status)
		}
		card := socketCard(t, h)
		if card.Value != "2 of 3 answering" || card.Level != tt.level || !strings.HasSuffix(card.Sub, "Spam filter") {
			t.Errorf("action %q: card = %+v", tt.action, card)
		}

		// Health: the row is in the table with its detail under the path.
		out := renderHealth(t, h, c, "")
		if want := `<span class="sp-muted sp-mono sp-small">` + dead + `</span><br><span class="sp-muted sp-small">` + refused + `</span>`; !strings.Contains(out, want) {
			t.Errorf("action %q: Health is missing the path with its detail", tt.action)
		}

		// Overview: the stamp takes the card's level and the lead names the card.
		rec := httptest.NewRecorder()
		h.HandleOverviewFragment(rec, reqAs("/overview/fragment", globalPrincipal))
		page := rec.Body.String()
		for _, want := range []string{`>` + tt.stamp + `<`, `Milter sockets</a> (2 of 3 answering)`} {
			if !strings.Contains(page, want) {
				t.Errorf("action %q: Overview is missing %q", tt.action, want)
			}
		}
	}
}

// Only the global administrator sees the address: Health and Overview are
// global-only, and a domain administrator gets none of them.
func TestFilterAddressIsGlobalOnly(t *testing.T) {
	h, _ := settingsServer(t)
	h.machine = &health.MachineSampler{}
	h.cfg.InboundEnabled = true
	h.cfg.InboundAntispamMilter, h.cfg.InboundAntispamAction = "inet:antispam-secret-name:11332", "accept"
	domainAdmin := auth.Principal{ID: 2, Username: "dom", Role: auth.RoleDomain, Domains: []int64{1}}

	for name, handler := range map[string]func(*httptest.ResponseRecorder, *Handlers){
		"overview": func(w *httptest.ResponseRecorder, h *Handlers) { h.HandleOverview(w, reqAs("/overview", domainAdmin)) },
		"overview fragment": func(w *httptest.ResponseRecorder, h *Handlers) {
			h.HandleOverviewFragment(w, reqAs("/overview/fragment", domainAdmin))
		},
		"health": func(w *httptest.ResponseRecorder, h *Handlers) {
			h.HandleHealth(w, reqAs("/server/health", domainAdmin))
		},
		"health fragment": func(w *httptest.ResponseRecorder, h *Handlers) {
			h.HandleHealthFragment(w, reqAs("/server/health/fragment", domainAdmin))
		},
	} {
		rec := httptest.NewRecorder()
		handler(rec, h)
		if rec.Code == 200 || strings.Contains(rec.Body.String(), "antispam-secret-name") {
			t.Errorf("%s answers a domain administrator with %d / carries the address", name, rec.Code)
		}
	}
}

func reqAs(path string, p auth.Principal) *http.Request {
	return auth.RequestWithPrincipal(httptest.NewRequest("GET", path, nil), p)
}
