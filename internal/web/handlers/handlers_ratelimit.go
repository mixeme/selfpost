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
	clear          bool
	mode           string
	ips            []string
	maxMessages    int
	windowSeconds  int
	autoMultiplier float64
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

func parseAutoMultiplier(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return store.DefaultAutoMultiplier, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("enter a valid multiplier (%.1f–%.1f)", store.MinAutoMultiplier, store.MaxAutoMultiplier)
	}
	if v < store.MinAutoMultiplier || v > store.MaxAutoMultiplier {
		return 0, fmt.Errorf("multiplier must be between %.1f and %.1f", store.MinAutoMultiplier, store.MaxAutoMultiplier)
	}
	return v, nil
}

func parseRateLimitMode(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return store.RateLimitModeManual, nil
	}
	if raw != store.RateLimitModeManual && raw != store.RateLimitModeAuto {
		return "", fmt.Errorf("choose manual or auto mode")
	}
	return raw, nil
}

func parseDomainRateLimitForm(r *http.Request, l1Max int) (rateLimitInput, error) {
	if err := r.ParseForm(); err != nil {
		return rateLimitInput{}, fmt.Errorf("invalid form submission")
	}
	if r.PostFormValue("clear") != "" {
		return rateLimitInput{clear: true}, nil
	}
	mode, err := parseRateLimitMode(r.PostFormValue("mode"))
	if err != nil {
		return rateLimitInput{}, err
	}
	if mode == store.RateLimitModeAuto {
		mult, err := parseAutoMultiplier(r.PostFormValue("auto_multiplier"))
		if err != nil {
			return rateLimitInput{}, err
		}
		return rateLimitInput{mode: mode, autoMultiplier: mult}, nil
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
	return rateLimitInput{mode: store.RateLimitModeManual, maxMessages: maxMessages, windowSeconds: windowSeconds}, nil
}

func parseAppRateLimitForm(r *http.Request, l1Max int) (rateLimitInput, error) {
	if err := r.ParseForm(); err != nil {
		return rateLimitInput{}, fmt.Errorf("invalid form submission")
	}
	if r.PostFormValue("clear") != "" {
		return rateLimitInput{clear: true}, nil
	}
	mode, err := parseRateLimitMode(r.PostFormValue("mode"))
	if err != nil {
		return rateLimitInput{}, err
	}

	if mode == store.RateLimitModeAuto {
		mult, err := parseAutoMultiplier(r.PostFormValue("auto_multiplier"))
		if err != nil {
			return rateLimitInput{}, err
		}
		return rateLimitInput{mode: mode, autoMultiplier: mult}, nil
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
	return rateLimitInput{mode: store.RateLimitModeManual, maxMessages: maxMessages, windowSeconds: windowSeconds}, nil
}

func parseAppAuthIPsForm(r *http.Request) (bool, []string, error) {
	if err := r.ParseForm(); err != nil {
		return false, nil, fmt.Errorf("invalid form submission")
	}
	restrict := r.PostFormValue("auth_ip_restrict") != ""
	if !restrict {
		return false, nil, nil
	}
	ips, err := parseIPList(r.PostFormValue("auth_allowed_ips"))
	if err != nil {
		return false, nil, err
	}
	if len(ips) == 0 {
		return false, nil, fmt.Errorf("enter at least one client IP when the allow-list is enabled")
	}
	return true, ips, nil
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
	if err := h.applyDomainRateLimit(in, d.ID); err != nil {
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
	in, err := parseAppRateLimitForm(r, h.l1Messages())
	if err != nil {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: fmt.Sprintf("%s: %s", a.Login, err.Error()),
		})
		return
	}
	if err := h.applyAppRateLimit(in, a.ID); err != nil {
		logf("panel: application %d: save rate limit: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?ratelimit=1", a.DomainID), http.StatusSeeOther)
}

func (h *Handlers) HandleAppAuthIPs(w http.ResponseWriter, r *http.Request) {
	a, ok := h.lookupApplication(w, r)
	if !ok {
		return
	}
	d, err := h.domains.Get(a.DomainID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	restrict, ips, err := parseAppAuthIPsForm(r)
	if err != nil {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: fmt.Sprintf("%s: %s", a.Login, err.Error()),
		})
		return
	}
	if err := h.apps.UpdateAuthIPs(a.ID, restrict, ips); err != nil {
		logf("panel: application %d: save auth IPs: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?authips=1", a.DomainID), http.StatusSeeOther)
}

func (h *Handlers) HandleDomainRateLimitRecalc(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	if err := h.recalcRateLimit(store.RateLimitScopeDomain, d.ID); err != nil {
		logf("panel: domain %d: recalc rate limit: %v", d.ID, err)
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: err.Error(),
		})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?recalculated=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleAppRateLimitRecalc(w http.ResponseWriter, r *http.Request) {
	a, ok := h.lookupApplication(w, r)
	if !ok {
		return
	}
	if err := h.recalcRateLimit(store.RateLimitScopeApp, a.ID); err != nil {
		d, _ := h.domains.Get(a.DomainID)
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: fmt.Sprintf("%s: %s", a.Login, err.Error()),
		})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?recalculated=1", a.DomainID), http.StatusSeeOther)
}

func (h *Handlers) recalcRateLimit(scope string, refID int64) error {
	return h.store.RecalcAutoRateLimit(scope, refID, h.sendLogRetentionDays(), h.l1Messages(), h.l1Window())
}

func (h *Handlers) applyDomainRateLimit(in rateLimitInput, domainID int64) error {
	if in.clear {
		return h.domains.ClearRateLimit(domainID)
	}
	rl := store.RateLimit{
		Scope:          store.RateLimitScopeDomain,
		RefID:          domainID,
		Mode:           in.mode,
		MaxMessages:    in.maxMessages,
		WindowSeconds:  in.windowSeconds,
		AutoMultiplier: in.autoMultiplier,
	}
	if in.mode == store.RateLimitModeAuto {
		rl.WindowSeconds = h.l1Window()
		if err := h.domains.SaveRateLimit(domainID, rl); err != nil {
			return err
		}
		return h.recalcRateLimit(store.RateLimitScopeDomain, domainID)
	}
	return h.domains.SaveRateLimit(domainID, rl)
}

func (h *Handlers) applyAppRateLimit(in rateLimitInput, appID int64) error {
	if in.clear {
		return h.apps.ClearRateLimit(appID)
	}
	rl := store.RateLimit{
		Scope:          store.RateLimitScopeApp,
		RefID:          appID,
		Mode:           in.mode,
		MaxMessages:    in.maxMessages,
		WindowSeconds:  in.windowSeconds,
		AutoMultiplier: in.autoMultiplier,
	}
	if in.mode == store.RateLimitModeAuto {
		rl.WindowSeconds = h.l1Window()
		if err := h.apps.SaveRateLimit(appID, rl); err != nil {
			return err
		}
		return h.recalcRateLimit(store.RateLimitScopeApp, appID)
	}
	return h.apps.SaveRateLimit(appID, rl)
}
