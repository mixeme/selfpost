package handlers

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
)

const defaultRateLimitWindowSeconds = 3600

type rateLimitInput struct {
	clear         bool
	ips           []string
	maxMessages   int
	windowSeconds int
}

func (h *Handlers) l1Messages() int {
	if h.cfg.RateLimitMessagesPerIP > 0 {
		return h.cfg.RateLimitMessagesPerIP
	}
	return 100
}

func (h *Handlers) l1Window() int {
	if h.cfg.RateLimitWindowSeconds > 0 {
		return h.cfg.RateLimitWindowSeconds
	}
	return defaultRateLimitWindowSeconds
}

func parseDomainRateLimitForm(r *http.Request, l1Max int) (rateLimitInput, error) {
	if err := r.ParseForm(); err != nil {
		return rateLimitInput{}, fmt.Errorf("invalid form submission")
	}
	if r.PostFormValue("clear") != "" {
		return rateLimitInput{clear: true}, nil
	}
	rawMax := strings.TrimSpace(r.PostFormValue("max_messages"))
	if rawMax == "" {
		return rateLimitInput{clear: true}, nil
	}
	maxMessages, err := parsePositiveInt(rawMax, 0)
	if err != nil || maxMessages <= 0 {
		return rateLimitInput{}, fmt.Errorf("enter a message limit greater than zero")
	}
	if maxMessages > l1Max {
		return rateLimitInput{}, fmt.Errorf("message limit cannot exceed the level-1 backstop (%d)", l1Max)
	}
	windowSeconds, err := parsePositiveInt(r.PostFormValue("window_seconds"), defaultRateLimitWindowSeconds)
	if err != nil || windowSeconds <= 0 {
		return rateLimitInput{}, fmt.Errorf("enter a time window greater than zero seconds")
	}
	return rateLimitInput{maxMessages: maxMessages, windowSeconds: windowSeconds}, nil
}

func parseAppRateLimitForm(r *http.Request, l1Max, domainMax int, domainActive bool) (rateLimitInput, error) {
	if err := r.ParseForm(); err != nil {
		return rateLimitInput{}, fmt.Errorf("invalid form submission")
	}
	if r.PostFormValue("clear") != "" {
		return rateLimitInput{clear: true}, nil
	}
	rawMax := strings.TrimSpace(r.PostFormValue("max_messages"))
	if rawMax == "" {
		return rateLimitInput{clear: true}, nil
	}
	ips, err := parseIPList(r.PostFormValue("allowed_ips"))
	if err != nil {
		return rateLimitInput{}, err
	}
	if len(ips) == 0 {
		return rateLimitInput{}, fmt.Errorf("enter at least one trusted client IP for an application override")
	}
	maxMessages, err := parsePositiveInt(rawMax, 0)
	if err != nil || maxMessages <= 0 {
		return rateLimitInput{}, fmt.Errorf("enter a message limit greater than zero")
	}
	if maxMessages > l1Max {
		return rateLimitInput{}, fmt.Errorf("message limit cannot exceed the level-1 backstop (%d)", l1Max)
	}
	if domainActive && maxMessages <= domainMax {
		return rateLimitInput{}, fmt.Errorf("application override must be greater than the domain limit (%d)", domainMax)
	}
	windowSeconds, err := parsePositiveInt(r.PostFormValue("window_seconds"), defaultRateLimitWindowSeconds)
	if err != nil || windowSeconds <= 0 {
		return rateLimitInput{}, fmt.Errorf("enter a time window greater than zero seconds")
	}
	return rateLimitInput{ips: ips, maxMessages: maxMessages, windowSeconds: windowSeconds}, nil
}

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

func parsePositiveInt(raw string, def int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	return strconv.Atoi(raw)
}

func (h *Handlers) HandleDomainRateLimit(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	in, err := parseDomainRateLimitForm(r, h.l1Messages())
	if err != nil {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: err.Error(),
		})
		return
	}
	if err := h.applyRateLimit(in, h.domains.SaveRateLimit, h.domains.ClearRateLimit, d.ID); err != nil {
		logf("panel: domain %d: save rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?ratelimit=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleAppRateLimit(w http.ResponseWriter, r *http.Request) {
	a, ok := h.lookupApplication(w, r)
	if !ok {
		return
	}
	d, err := h.domains.Get(a.DomainID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	domainRL, domainOK, err := h.domains.RateLimit(d.ID)
	if err != nil {
		logf("panel: domain %d: rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	domainActive := domainOK && domainRL.Active()
	in, err := parseAppRateLimitForm(r, h.l1Messages(), domainRL.MaxMessages, domainActive)
	if err != nil {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: fmt.Sprintf("%s: %s", a.Login, err.Error()),
		})
		return
	}
	if err := h.applyRateLimit(in, h.apps.SaveRateLimit, h.apps.ClearRateLimit, a.ID); err != nil {
		logf("panel: application %d: save rate limit: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?ratelimit=1", a.DomainID), http.StatusSeeOther)
}

func (h *Handlers) applyRateLimit(
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
