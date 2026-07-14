package web

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"codeberg.org/mix/selfpost/internal/store"
)

// defaultRateLimitWindowSeconds is the sliding-window length used when an admin
// sets a message ceiling but leaves the window blank (spec 7.4, matching the
// level-1 default hour, spec 8: RATE_LIMIT_WINDOW_SECONDS).
const defaultRateLimitWindowSeconds = 3600

// rateLimitInput is the validated result of a rate-limit form submission. clear
// means "remove the differentiated limit" (spec 7.4: an empty IP binding leaves
// only level 1).
type rateLimitInput struct {
	clear         bool
	ips           []string
	maxMessages   int
	windowSeconds int
}

// parseRateLimitForm validates a rate-limit submission on the server (spec
// 7.6.2). It returns clear=true when the admin removes the limit or leaves the
// IP binding empty; otherwise it requires a positive ceiling and window. The
// returned error's message is safe to show to the admin.
func parseRateLimitForm(r *http.Request) (rateLimitInput, error) {
	if err := r.ParseForm(); err != nil {
		return rateLimitInput{}, fmt.Errorf("invalid form submission")
	}
	if r.PostFormValue("clear") != "" {
		return rateLimitInput{clear: true}, nil
	}
	ips, err := parseIPList(r.PostFormValue("allowed_ips"))
	if err != nil {
		return rateLimitInput{}, err
	}
	if len(ips) == 0 {
		// No IP binding: the differentiated limit does not apply (spec 7.4).
		return rateLimitInput{clear: true}, nil
	}
	maxMessages, err := parsePositiveInt(r.PostFormValue("max_messages"), 0)
	if err != nil || maxMessages <= 0 {
		return rateLimitInput{}, fmt.Errorf("enter a message limit greater than zero")
	}
	windowSeconds, err := parsePositiveInt(r.PostFormValue("window_seconds"), defaultRateLimitWindowSeconds)
	if err != nil || windowSeconds <= 0 {
		return rateLimitInput{}, fmt.Errorf("enter a time window greater than zero seconds")
	}
	return rateLimitInput{ips: ips, maxMessages: maxMessages, windowSeconds: windowSeconds}, nil
}

// parseIPList parses the allowed-IP field (IPs separated by newlines, commas or
// whitespace) into a deduplicated list of canonical addresses, rejecting any
// token that is not a valid IP (spec 7.6.2). The values are only ever stored as
// SQLite parameters and compared in the milter, never written to a config file.
func parseIPList(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	var out []string
	seen := make(map[string]bool)
	for _, f := range fields {
		ip := net.ParseIP(f)
		if ip == nil {
			return nil, fmt.Errorf("%q is not a valid IP address", f)
		}
		c := ip.String()
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// parsePositiveInt parses a trimmed integer field, returning def when it is
// blank. A non-numeric value returns an error.
func parsePositiveInt(raw string, def int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	return strconv.Atoi(raw)
}

// handleDomainRateLimit saves or clears a domain-level differentiated rate limit
// (spec 7.4). No reload is needed — the milter reads the row live.
func (s *Server) handleDomainRateLimit(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lookupDomain(w, r)
	if !ok {
		return
	}
	in, err := parseRateLimitForm(r)
	if err != nil {
		s.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: err.Error(),
		})
		return
	}
	if err := s.applyRateLimit(in, s.domains.SaveRateLimit, s.domains.ClearRateLimit, d.ID); err != nil {
		logf("panel: domain %d: save rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?ratelimit=1", d.ID), http.StatusSeeOther)
}

// handleAppRateLimit saves or clears an application-level differentiated rate
// limit (spec 7.4).
func (s *Server) handleAppRateLimit(w http.ResponseWriter, r *http.Request) {
	a, ok := s.lookupApplication(w, r)
	if !ok {
		return
	}
	d, err := s.domains.Get(a.DomainID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	in, err := parseRateLimitForm(r)
	if err != nil {
		s.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: fmt.Sprintf("%s: %s", a.Login, err.Error()),
		})
		return
	}
	if err := s.applyRateLimit(in, s.apps.SaveRateLimit, s.apps.ClearRateLimit, a.ID); err != nil {
		logf("panel: application %d: save rate limit: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?ratelimit=1", a.DomainID), http.StatusSeeOther)
}

// applyRateLimit dispatches a validated input to the save or clear method of the
// relevant service, keyed by the domain or application id.
func (s *Server) applyRateLimit(
	in rateLimitInput,
	save func(id int64, ips []string, maxMessages, windowSeconds int) error,
	clear func(id int64) error,
	id int64,
) error {
	if in.clear {
		return clear(id)
	}
	return save(id, in.ips, in.maxMessages, in.windowSeconds)
}
