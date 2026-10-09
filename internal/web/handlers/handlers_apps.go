package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
)

// newCred carries a freshly generated login/password to the template so it can
// be shown exactly once (security.md). It is never read back from storage.
type newCred struct {
	Login    string
	Password string
}

// detailView holds the one-shot, request-specific extras layered on top of a
// domain's persistent state when rendering its page: an application-form error,
// the values to repopulate that form, and any just-issued credential to show
// once.
type detailView struct {
	FormErr   string
	FormLogin string
	FormMode  string
	FormAddrs string
	NewCred   *newCred
	// RateLimitErr surfaces a validation error from a domain- or
	// application-level rate-limit form (guide § Rate limiting) as a page
	// banner.
	RateLimitErr string
	// ExportErr surfaces a rejected encryption password from the export card.
	ExportErr string
}

// appRateLimitView pairs an application with its differentiated rate-limit
// settings for the domain page. store.Application is embedded so the existing
// template fields (Login, AddressMode, Addresses, ID) resolve unchanged.
type appRateLimitView struct {
	store.Application
	HasLimit       bool
	AuthIPsText    string
	MaxText        string
	WindowVal      string
	Mode           string
	AutoMultiplier string
	AutoUpdated    string
	IsAuto         bool
	// RLMode is the form's limit choice: "domain" (no limit of its own),
	// "manual" or "auto".
	RLMode string
	Stats  sendStatsView
}

// sendStatsView is the template-facing send statistics block.
type sendStatsView struct {
	Total       int64
	PeakPerHour int64
	AvgPerHour  string
}

// HandleDomainDetail shows a single domain: its DKIM DNS record (product.md)
// and its applications with the controls to add, edit, delete and re-issue
// credentials (product.md).
func (h *Handlers) HandleDomainDetail(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	h.renderDomainDetail(w, r, http.StatusOK, d, detailView{FormMode: store.AddressModeWildcard})
}

// renderDomainDetail renders the domain page. view supplies request-specific
// extras (form error/values, a one-time credential); everything else is loaded
// fresh from the stores so the page always reflects committed state.
func (h *Handlers) renderDomainDetail(w http.ResponseWriter, r *http.Request, status int, d store.Domain, view detailView) {
	record, err := h.domains.DKIMRecord(d)
	if err != nil {
		logf("panel: domain %d: dkim record: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	apps, err := h.apps.List(d.ID)
	if err != nil {
		logf("panel: domain %d: list applications: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	appViews := make([]appRateLimitView, 0, len(apps))
	retention := h.sendLogRetentionDays()
	for _, a := range apps {
		rl, ok, err := h.apps.RateLimit(a.ID)
		if err != nil {
			logf("panel: application %d: rate limit: %v", a.ID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		appStats, err := h.store.AppSendStats(a.Login, retention, a.CreatedAt)
		if err != nil {
			logf("panel: application %d: send stats: %v", a.ID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		mode := store.RateLimitModeManual
		if ok && rl.Mode != "" {
			mode = rl.Mode
		}
		mult := rl.AutoMultiplier
		if mult <= 0 {
			mult = store.DefaultAutoMultiplier
		}
		appViews = append(appViews, appRateLimitView{
			Application:    a,
			HasLimit:       ok && rl.Active(),
			AuthIPsText:    strings.Join(a.AuthAllowedIPs, "\n"),
			MaxText:        intOrBlank(rl.MaxMessages),
			WindowVal:      windowOrDefault(rl.WindowSeconds),
			Mode:           mode,
			AutoMultiplier: formatMultiplier(mult),
			IsAuto:         ok && rl.IsAuto(),
			RLMode:         limitChoice(rl, ok),
			AutoUpdated:    formatAutoUpdated(rl.AutoUpdatedAt),
			Stats:          formatSendStats(appStats),
		})
	}

	domainRL, domainRLok, err := h.domains.RateLimit(d.ID)
	if err != nil {
		logf("panel: domain %d: rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	domainStats, err := h.store.DomainSendStats(d.Name, retention, d.CreatedAt)
	if err != nil {
		logf("panel: domain %d: send stats: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	domainMode := store.RateLimitModeManual
	domainMult := store.DefaultAutoMultiplier
	if domainRLok {
		if domainRL.Mode != "" {
			domainMode = domainRL.Mode
		}
		if domainRL.AutoMultiplier > 0 {
			domainMult = domainRL.AutoMultiplier
		}
	}

	// What DNS actually publishes for the domain today, checked against the key
	// this server signs with. Cached by the checker, so re-rendering the page
	// after a form post costs nothing.
	reportEmail, err := h.domainReportAddress(d)
	if err != nil {
		logf("panel: domain %d: dmarc report address: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	dns, srv := h.domainDNS(d, record, reportEmail, false)
	reportAuthName, reportAuthValue, needsReportAuth := dnscheck.ExternalReportAuth(d.Name, reportEmail)
	hostedAddr := h.hostedDMARCAddress(d.Name)
	// The form's "inherit" is a domain that follows a user's default. What it
	// offers next to that choice is the address following would give: the
	// followed user's when the domain already follows one, otherwise the
	// signed-in user's own — choosing it makes the domain follow them.
	dmarcMode := "inherit"
	dmarcCustom := ""
	profileEmail := reportEmail
	if !d.DMARCRuaUserID.Valid {
		profileEmail = ""
		if p, ok := h.principal(r); ok {
			if def, err := h.store.GetDMARCDefault(p.ID); err == nil {
				profileEmail = def.Resolve(hostedAddr)
			}
		}
		switch {
		case d.DMARCRua == "":
			dmarcMode = "none"
		case hostedAddr != "" && strings.EqualFold(d.DMARCRua, hostedAddr):
			dmarcMode = "hosted"
		default:
			dmarcMode = "custom"
			dmarcCustom = d.DMARCRua
		}
	}
	dmarcSource := "policy"
	switch {
	case dmarcMode == "hosted":
		dmarcSource = "hosted"
	case dmarcMode == "custom":
		dmarcSource = "custom"
	case dmarcMode == "none":
		dmarcSource = "none"
	case reportEmail != "":
		dmarcSource = "settings"
	}

	data := h.pageBase(r)
	data["Title"] = "SelfPost — " + d.Name
	data["Active"] = "domains"
	data["Domain"] = d
	data["Record"] = record
	data["DNS"] = dns
	data["SPFExample"] = dnscheck.SPFExample(h.cfg.Hostname, srv.IPs)
	data["DMARCName"] = dnscheck.DMARCRecordName(d.Name)
	data["DMARCExample"] = dnscheck.DMARCExample(reportEmail)
	data["DMARCSource"] = dmarcSource
	data["ProfileDMARCEmail"] = profileEmail
	data["ResolvedDMARCEmail"] = reportEmail
	data["DMARCRuaMode"] = dmarcMode
	data["DMARCRuaCustom"] = dmarcCustom
	data["HostedDMARCEmail"] = hostedAddr
	data["DMARCIngestEnabled"] = h.cfg.DMARCEnabled
	data["ReportAuthName"] = reportAuthName
	data["ReportAuthValue"] = reportAuthValue
	data["NeedsReportAuth"] = needsReportAuth
	data["SameDomainRUA"] = reportEmail != "" && strings.EqualFold(dnscheck.EmailDomain(reportEmail), d.Name) &&
		!(h.cfg.DMARCEnabled && dmarc.IsHostedOnHostname(reportEmail, h.cfg.Hostname))
	data["Hostname"] = h.cfg.Hostname
	data["SubmissionEnabled"] = h.cfg.SubmissionEnabled
	data["Apps"] = appViews
	data["Error"] = view.FormErr
	data["FormLogin"] = view.FormLogin
	data["FormMode"] = view.FormMode
	data["FormAddrs"] = view.FormAddrs
	data["NewCred"] = view.NewCred
	data["Flash"] = detailFlash(r)
	data["Wildcard"] = store.AddressModeWildcard
	data["List"] = store.AddressModeList
	data["RateLimitErr"] = view.RateLimitErr
	data["ExportErr"] = view.ExportErr
	data["MinPwLen"] = validate.MinSecretFilePasswordLen
	data["DomainHasRL"] = domainRLok && domainRL.Active()
	data["DomainRLMax"] = intOrBlank(domainRL.MaxMessages)
	data["DomainRLWin"] = windowOrDefault(domainRL.WindowSeconds)
	data["DomainRLMaxNum"] = domainRL.MaxMessages
	data["DomainRLMode"] = domainMode
	data["DomainRLAuto"] = domainRLok && domainRL.IsAuto()
	data["DomainRLMultiplier"] = formatMultiplier(domainMult)
	data["DomainRLAutoUpdated"] = formatAutoUpdated(domainRL.AutoUpdatedAt)
	data["DomainStats"] = formatSendStats(domainStats)
	data["StatsWindowDays"] = domainStats.WindowDays
	data["StatsRetentionWarning"] = retention < store.StatsWindowDays
	data["L1Messages"] = h.l1Messages()
	data["L1Window"] = h.l1Window()
	data["DefaultAutoMultiplier"] = store.DefaultAutoMultiplier
	data["MinAutoMultiplier"] = store.MinAutoMultiplier
	data["MaxAutoMultiplier"] = store.MaxAutoMultiplier
	h.view.Render(w, status, "domain_detail", data)
}

// domainDNS resolves what the world sees for a domain: its DKIM, SPF and DMARC
// records. The server's own address comes from the (separately
// cached) hostname check, so the SPF heuristic knows which IP it is looking for
// and no extra environment variable is needed. That server result is returned
// alongside, because the page's suggested SPF record is built from the same
// addresses. force bypasses the cache, for the Re-check button.
func (h *Handlers) domainDNS(d store.Domain, record domain.DKIMRecord, reportEmail string, force bool) (dnscheck.Domain, dnscheck.Server) {
	srv := h.dns.Server(h.cfg.Hostname, false)
	return h.dns.Domain(dnscheck.Query{
		Name:             d.Name,
		Selector:         d.DKIMSelector,
		ExpectedDKIM:     record.Value,
		Hostname:         srv.Hostname,
		ServerIPs:        srv.IPs,
		DMARCReportEmail: reportEmail,
	}, force), srv
}

// HandleDomainDNSRecheck re-runs the domain's DNS checks ignoring the cache and
// returns to its page, which then renders the fresh result.
func (h *Handlers) HandleDomainDNSRecheck(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	record, err := h.domains.DKIMRecord(d)
	if err != nil {
		logf("panel: domain %d: dkim record: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	reportEmail, err := h.domainReportAddress(d)
	if err != nil {
		logf("panel: domain %d: dmarc report address: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.domainDNS(d, record, reportEmail, true)
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?rechecked=1", d.ID), http.StatusSeeOther)
}

// limitChoice names which of the form's three limit choices an application has:
// the domain limit (none of its own), manual, or auto.
func limitChoice(rl store.RateLimit, configured bool) string {
	switch {
	case !configured:
		return "domain"
	case rl.IsAuto():
		return store.RateLimitModeAuto
	default:
		return store.RateLimitModeManual
	}
}

// intOrBlank renders a non-positive number as an empty string so an unset field
// shows blank rather than "0".
func intOrBlank(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// windowOrDefault renders the window seconds, substituting the default when
// unset so the form always suggests a sensible value.
func windowOrDefault(n int) string {
	if n <= 0 {
		return strconv.Itoa(defaultRateLimitWindowSeconds)
	}
	return strconv.Itoa(n)
}

func formatSendStats(s store.SendStats) sendStatsView {
	return sendStatsView{
		Total:       s.Total,
		PeakPerHour: s.PeakPerHour,
		AvgPerHour:  fmt.Sprintf("%.1f", s.AvgPerHour),
	}
}

func formatMultiplier(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func formatAutoUpdated(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// detailFlash maps a fixed redirect flag to a fixed message, so status text
// after a redirect is never attacker-influenced.
func detailFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Get("appdeleted") != "":
		return "Application deleted."
	case r.URL.Query().Get("appsaved") != "":
		return "Application saved."
	case r.URL.Query().Get("ratelimit") != "":
		return "Rate limit updated."
	case r.URL.Query().Get("recalculated") != "":
		return "Auto rate limit recalculated."
	case r.URL.Query().Get("dmarc") != "":
		return "DMARC report settings updated."
	case r.URL.Query().Get("imported") != "":
		return "Domain imported. Its DKIM DNS record is unchanged — no DNS update is needed."
	case r.URL.Query().Get("rechecked") != "":
		return "DNS re-checked."
	default:
		return ""
	}
}

// The application form is ONE post (plan § Routes): who the application may
// send as, which client IPs may use it and its rate limit are parsed and
// validated together and saved together, or not at all (app.Service
// CreateWithSettings / SaveSettings). The same form, empty, adds an
// application; filled, it edits one. The login is given once, at creation.

// parseApplicationForm reads the form's three parts. Every field is checked
// here or by the service before anything is written.
func (h *Handlers) parseApplicationForm(r *http.Request) (app.Settings, error) {
	set := app.Settings{
		Mode:      r.PostFormValue("mode"),
		Addresses: splitAddresses(r.PostFormValue("addresses")),
	}
	restrict, ips, err := parseAppAuthIPsForm(r)
	if err != nil {
		return app.Settings{}, err
	}
	set.AuthIPRestrict, set.AuthAllowedIPs = restrict, ips

	switch mode := strings.TrimSpace(r.PostFormValue("rl_mode")); mode {
	case "", "domain":
		// Use the domain limit: the application has none of its own.
	case store.RateLimitModeAuto:
		mult, err := parseAutoMultiplier(r.PostFormValue("auto_multiplier"))
		if err != nil {
			return app.Settings{}, err
		}
		set.Limit = app.Limit{Mode: mode, AutoMultiplier: mult, WindowSeconds: h.l1Window()}
	case store.RateLimitModeManual:
		maxMessages, err := parsePositiveInt(r.PostFormValue("max_messages"), 0)
		if err != nil || maxMessages <= 0 {
			return app.Settings{}, fmt.Errorf("enter a message limit greater than zero, or use the domain limit")
		}
		if l1 := h.l1Messages(); maxMessages > l1 {
			return app.Settings{}, fmt.Errorf("message limit cannot exceed the level-1 backstop (%d)", l1)
		}
		window, err := parsePositiveInt(r.PostFormValue("window_seconds"), defaultRateLimitWindowSeconds)
		if err != nil || window <= 0 {
			return app.Settings{}, fmt.Errorf("enter a time window greater than zero seconds")
		}
		set.Limit = app.Limit{Mode: mode, MaxMessages: maxMessages, WindowSeconds: window}
	default:
		return app.Settings{}, fmt.Errorf("choose the domain limit, manual or auto")
	}
	return set, nil
}

func (h *Handlers) recalcApp(appID int64) error {
	return h.recalcRateLimit(store.RateLimitScopeApp, appID)
}

// noStore keeps a page that shows a secret out of every cache and out of the
// browser's history copy: the password on it is shown once.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

// HandleApplicationNew shows the form that adds an application. Until the
// domain page is split (stage 2) the form is a card of that page.
func (h *Handlers) HandleApplicationNew(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	h.renderDomainDetail(w, r, http.StatusOK, d, detailView{FormMode: store.AddressModeWildcard})
}

// HandleApplicationCreate creates an application with all of its settings and
// answers with the generated password, shown once (product.md, security.md).
// The password cannot be recovered later, so the page is the response to this
// POST — it has no GET path — and is never stored by the browser.
func (h *Handlers) HandleApplicationCreate(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d,
			detailView{FormErr: "Invalid form submission.", FormMode: store.AddressModeWildcard})
		return
	}
	login := strings.TrimSpace(r.PostFormValue("login"))
	repopulate := detailView{
		FormLogin: login,
		FormMode:  r.PostFormValue("mode"),
		FormAddrs: r.PostFormValue("addresses"),
	}
	set, err := h.parseApplicationForm(r)
	if err != nil {
		repopulate.FormErr = err.Error()
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, repopulate)
		return
	}
	a, password, err := h.apps.CreateWithSettings(d.ID, login, set, h.recalcApp)
	if err != nil {
		repopulate.FormErr = applicationErrorMessage(err)
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrLoginExists) {
			status = http.StatusConflict
		}
		h.renderDomainDetail(w, r, status, d, repopulate)
		return
	}
	noStore(w)
	h.renderDomainDetail(w, r, http.StatusCreated, d, detailView{
		FormMode: store.AddressModeWildcard,
		NewCred:  &newCred{Login: a.Login, Password: password},
	})
}

// HandleApplicationEdit shows an application's form. Until the domain page is
// split (stage 2) that is the application's Edit panel on the domain page.
func (h *Handlers) HandleApplicationEdit(w http.ResponseWriter, r *http.Request) {
	_, d, ok := h.lookupDomainApplication(w, r)
	if !ok {
		return
	}
	h.renderDomainDetail(w, r, http.StatusOK, d, detailView{FormMode: store.AddressModeWildcard})
}

// HandleApplicationSave saves an application's form: sender, client IPs and
// rate limit in one step.
func (h *Handlers) HandleApplicationSave(w http.ResponseWriter, r *http.Request) {
	a, d, ok := h.lookupDomainApplication(w, r)
	if !ok {
		return
	}
	fail := func(msg string) {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:     store.AddressModeWildcard,
			RateLimitErr: fmt.Sprintf("%s: %s", a.Login, msg),
		})
	}
	if err := r.ParseForm(); err != nil {
		fail("invalid form submission")
		return
	}
	// The login is the SASL account's name and is not renamed by this form.
	if login := strings.TrimSpace(r.PostFormValue("login")); login != "" && login != a.Login {
		fail("the login of an application cannot be changed; add a new application instead")
		return
	}
	set, err := h.parseApplicationForm(r)
	if err != nil {
		fail(err.Error())
		return
	}
	if err := h.apps.SaveSettings(a.ID, set, h.recalcApp); err != nil {
		fail(applicationErrorMessage(err))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?appsaved=1", d.ID), http.StatusSeeOther)
}

// HandleRegenPassword issues a new password for an application and shows it once
// (product.md, security.md): the response to the POST, never cached.
func (h *Handlers) HandleRegenPassword(w http.ResponseWriter, r *http.Request) {
	a, d, ok := h.lookupDomainApplication(w, r)
	if !ok {
		return
	}
	password, err := h.apps.RegeneratePassword(a.ID)
	if err != nil {
		logf("panel: regenerate password for application %d: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	noStore(w)
	h.renderDomainDetail(w, r, http.StatusOK, d, detailView{
		FormMode: store.AddressModeWildcard,
		NewCred:  &newCred{Login: a.Login, Password: password},
	})
}

// HandleDeleteApplication removes an application and returns to its domain page
// (product.md).
func (h *Handlers) HandleDeleteApplication(w http.ResponseWriter, r *http.Request) {
	a, _, ok := h.lookupDomainApplication(w, r)
	if !ok {
		return
	}
	if err := h.apps.Delete(a.ID); err != nil {
		logf("panel: delete application %d: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?appdeleted=1", a.DomainID), http.StatusSeeOther)
}

// lookupDomainApplication resolves /outbound/domains/{id}/applications/{aid}
// to the application and its domain. The application must belong to the domain
// named in the path: an id from another domain is answered 404 like a missing
// one, whoever asks, so a URL can never address an application through a
// domain it is not in.
func (h *Handlers) lookupDomainApplication(w http.ResponseWriter, r *http.Request) (store.Application, store.Domain, bool) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return store.Application{}, store.Domain{}, false
	}
	a, ok := h.lookupApplication(w, r)
	if !ok {
		return store.Application{}, store.Domain{}, false
	}
	if a.DomainID != d.ID {
		http.NotFound(w, r)
		return store.Application{}, store.Domain{}, false
	}
	return a, d, true
}

// lookupApplication resolves the {aid} path value to an application, writing a
// 404 for a bad id or missing application.
func (h *Handlers) lookupApplication(w http.ResponseWriter, r *http.Request) (store.Application, bool) {
	id, err := strconv.ParseInt(r.PathValue("aid"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return store.Application{}, false
	}
	a, err := h.apps.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrApplicationNotFound) {
			http.NotFound(w, r)
			return store.Application{}, false
		}
		logf("panel: get application %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Application{}, false
	}
	p, ok := h.principal(r)
	if !ok || !p.CanAccessApp(a) {
		http.NotFound(w, r)
		return store.Application{}, false
	}
	return a, true
}

// splitAddresses turns the textarea/field input (addresses separated by
// newlines, commas or whitespace) into a raw slice. Normalisation and
// validation happen in the app service (security.md).
func splitAddresses(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' || r == ';'
	})
}

// applicationErrorMessage turns a service error into a user-facing message,
// passing through the validation errors (which are safe, fixed strings) and
// masking anything unexpected.
func applicationErrorMessage(err error) string {
	switch {
	case errors.Is(err, store.ErrLoginExists):
		return "That login is already in use. Choose another."
	case errors.Is(err, store.ErrDomainNotFound), errors.Is(err, store.ErrApplicationNotFound):
		return "The item no longer exists."
	default:
		// Validation errors from the app service are safe to surface verbatim;
		// they describe what the admin must fix (login/address rules).
		return err.Error()
	}
}
