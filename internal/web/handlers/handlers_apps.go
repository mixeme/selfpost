package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
)

// HandleDomainDetail shows a single domain: the DNS records it must publish and
// what DNS says about them, its applications and how to connect (product.md).
func (h *Handlers) HandleDomainDetail(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	h.renderDomain(w, r, http.StatusOK, d)
}

// renderDomain renders the domain page. Everything is loaded fresh from the
// stores, so the page always reflects committed state; the DNS state is cached
// by the checker, so re-rendering after a redirect costs nothing.
func (h *Handlers) renderDomain(w http.ResponseWriter, r *http.Request, status int, d store.Domain) {
	p, _ := h.principal(r)
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
	retention := h.sendLogRetentionDays()
	rows := make([]view.OutAppRow, 0, len(apps))
	for _, a := range apps {
		row, err := h.outAppRow(d, a, retention)
		if err != nil {
			logf("panel: application %d: %v", a.ID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		rows = append(rows, row)
	}
	domainStats, err := h.store.DomainSendStats(d.Name, retention, d.CreatedAt)
	if err != nil {
		logf("panel: domain %d: send stats: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	domainRL, domainRLok, err := h.domains.RateLimit(d.ID)
	if err != nil {
		logf("panel: domain %d: rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// What DNS actually publishes for the domain today, checked against the key
	// this server signs with.
	reportEmail, err := h.domainReportAddress(d)
	if err != nil {
		logf("panel: domain %d: dmarc report address: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	dns, srv := h.domainDNS(d, record, reportEmail, false)
	records := []view.Record{
		view.DKIMRecord(d.DKIMSelector, record.Name, record.Value, dnsCheck(dns.DKIM)),
		view.SPFRecord(d.Name, dnscheck.SPFExample(h.cfg.Hostname, srv.IPs), dnsCheck(dns.SPF)),
	}
	reports := ""
	if h.cfg.DMARCEnabled && reportEmail != "" {
		reports = "/outbound/dmarc/domains/" + strconv.FormatInt(d.ID, 10)
	}
	records = append(records, view.DMARCRecord(view.DMARCRecordInput{
		Host: dnscheck.DMARCRecordName(d.Name), Value: dnscheck.DMARCExample(reportEmail), Check: dnsCheck(dns.DMARC),
		Source: h.reportSource(d, reportEmail), SettingsHref: view.DomainHref(d.ID) + "/settings", ReportsHref: reports,
		SameDomain: reportEmail != "" && strings.EqualFold(dnscheck.EmailDomain(reportEmail), d.Name) &&
			!(h.cfg.DMARCEnabled && dmarc.IsHostedOnHostname(reportEmail, h.cfg.Hostname)),
	}))
	if name, value, needs := dnscheck.ExternalReportAuth(d.Name, reportEmail); needs {
		records = append(records, view.ReportAuthRecord(name, value, dnsCheck(dns.DMARCReportAuth)))
	}

	page := view.NewOutDomain(h.shellMeta(r), d.ID, d.Name, p.IsGlobal()).
		WithStats(domainStats.Total, domainStats.PeakPerHour, fmt.Sprintf("%.1f", domainStats.AvgPerHour), domainStats.WindowDays).
		WithRecords(records, view.FormatAge(dns.CheckedAt, time.Now())).
		WithApplications(rows).
		WithConnection(h.cfg.Hostname, h.cfg.SubmissionEnabled).
		WithSeeAlso("/outbound/log?domain="+url.QueryEscape(d.Name), reports).
		WithRateLimit(domainRLok && domainRL.IsAuto(), domainRLok && domainRL.Active() && !domainRL.IsAuto()).
		WithResult(detailFlash(r))
	h.view.Render(w, status, "out-domain", page)
}

// dnsCheck is a DNS result as the view reads it.
func dnsCheck(r dnscheck.Result) view.DNSCheck {
	return view.DNSCheck{Status: string(r.Status), Detail: r.Detail, Found: r.Records}
}

// reportSource says in words where a domain's DMARC reports go: the default of
// the user it follows, the hosted address, an address of its own, or nowhere.
func (h *Handlers) reportSource(d store.Domain, reportEmail string) string {
	switch {
	case d.DMARCRuaUserID.Valid:
		if def, err := h.store.GetDMARCDefault(d.DMARCRuaUserID.Int64); err == nil && def.Username != "" {
			return def.Username + "'s default"
		}
		return "a user's default"
	case reportEmail == "":
		return "none"
	case strings.EqualFold(reportEmail, h.hostedDMARCAddress(d.Name)):
		return "SelfPost hosted"
	}
	return "a custom address"
}

// outAppRow is one line of the Applications table of a domain.
func (h *Handlers) outAppRow(d store.Domain, a store.Application, retention int) (view.OutAppRow, error) {
	rl, ok, err := h.apps.RateLimit(a.ID)
	if err != nil {
		return view.OutAppRow{}, fmt.Errorf("rate limit: %w", err)
	}
	stats, err := h.store.AppSendStats(a.Login, retention, a.CreatedAt)
	if err != nil {
		return view.OutAppRow{}, fmt.Errorf("send stats: %w", err)
	}
	senders := a.Addresses
	if a.AddressMode == store.AddressModeWildcard {
		senders = []string{"*@" + d.Name}
	}
	limits := []view.Tag{{Label: "domain"}}
	if ok && rl.Active() {
		label := view.FormatRate(rl.MaxMessages, rl.WindowSeconds)
		if rl.IsAuto() {
			label = "auto · " + label
		}
		limits = []view.Tag{{Status: "ok", Label: label}}
	}
	if a.AuthIPRestrict {
		label := strconv.Itoa(len(a.AuthAllowedIPs)) + " IPs"
		if len(a.AuthAllowedIPs) == 1 {
			label = "1 IP"
		}
		limits = append(limits, view.Tag{Status: "ok", Label: label})
	}
	return view.OutAppRow{
		Login: a.Login, Senders: senders, Activity: view.FormatActivity(stats.Total, stats.PeakPerHour), Limits: limits,
		Edit: fmt.Sprintf("%s/applications/%d", view.DomainHref(d.ID), a.ID),
	}, nil
}

// settingsView is what a re-shown settings page carries besides the stored
// state: the refusal, and the report-address choice and custom address that
// were submitted (empty: show what is stored).
type settingsView struct {
	Err       string
	RuaMode   string
	RuaCustom string
}

// HandleDomainSettings shows the settings that are set once and rarely
// touched: where DMARC reports go, the domain's rate limit, the export and the
// delete entry.
func (h *Handlers) HandleDomainSettings(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	h.renderDomainSettings(w, r, http.StatusOK, d, settingsView{})
}

func (h *Handlers) renderDomainSettings(w http.ResponseWriter, r *http.Request, status int, d store.Domain, sv settingsView) {
	p, _ := h.principal(r)
	rl, ok, err := h.domains.RateLimit(d.ID)
	if err != nil {
		logf("panel: domain %d: rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	stats, err := h.store.DomainSendStats(d.Name, h.sendLogRetentionDays(), d.CreatedAt)
	if err != nil {
		logf("panel: domain %d: send stats: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mult := rl.AutoMultiplier
	if !ok || mult <= 0 {
		mult = store.DefaultAutoMultiplier
	}
	rate := view.DomainRateLimit{
		Active: ok && rl.Active(), Auto: ok && rl.IsAuto(),
		MaxMessages: intOrBlank(rl.MaxMessages), Window: windowOrDefault(rl.WindowSeconds), Multiplier: formatMultiplier(mult),
		Peak: stats.PeakPerHour, Updated: formatAutoUpdated(rl.AutoUpdatedAt),
		L1Messages: h.l1Messages(), L1Window: h.l1Window(),
		MinMultiplier: formatBound(store.MinAutoMultiplier), MaxMultiplier: formatBound(store.MaxAutoMultiplier),
		DefaultMultiplier: formatBound(store.DefaultAutoMultiplier),
	}
	if rate.Auto {
		rate.Computed = intOrBlank(rl.MaxMessages)
	}

	options, selected, custom, help := h.reportAddressForm(r, d)
	if sv.RuaMode != "" {
		selected, custom = sv.RuaMode, sv.RuaCustom
	}
	page := view.NewOutDomainSettings(h.shellMeta(r), d.ID, d.Name, d.AppCount, p.IsGlobal()).
		WithReportAddress(options, selected, custom, help).
		WithExport(validate.MinSecretFilePasswordLen).
		WithRateLimit(rate).
		WithResult("", sv.Err)
	h.view.Render(w, status, "out-domain-settings", page)
}

// reportAddressForm is the choices of the report-address select for a domain
// and the signed-in user. "inherit" makes the domain follow the default of the
// user who saves the form (HandleDomainDMARC), so what the option says depends
// on whom the domain follows now: the user themselves, someone else, or nobody.
// A domain that follows someone else also offers "keep" — that user's default,
// selected — so saving the form untouched does not take the domain over.
func (h *Handlers) reportAddressForm(r *http.Request, d store.Domain) (options []view.Option, selected, custom string, help view.Text) {
	hosted := h.hostedDMARCAddress(d.Name)
	me, _ := h.principal(r)
	mine, _ := h.store.GetDMARCDefault(me.ID)
	address := func(a string) string {
		if a == "" {
			return "no report address yet"
		}
		return a
	}
	myAddr := address(mine.Resolve(hosted))

	help = view.Rich("A default belongs to a user and is set under their ", view.Link("/account#dmarc", "Account"),
		"; a domain follows one named user, so two people sharing a domain never pull it two ways. Changing this changes the DMARC record to publish.")
	inherit := view.Option{Value: "inherit", Label: "My default — " + myAddr}
	followsOther := d.DMARCRuaUserID.Valid && d.DMARCRuaUserID.Int64 != me.ID
	switch {
	case followsOther:
		inherit.Label = "My default instead — " + myAddr + " (you are " + me.Username + ")"
		keep := view.Option{Value: "keep", Label: "Another user's default"}
		if other, err := h.store.GetDMARCDefault(d.DMARCRuaUserID.Int64); err == nil {
			keep.Label = other.Username + "'s default — " + address(other.Resolve(hosted))
		}
		options = append(options, keep)
	case d.DMARCRuaUserID.Valid:
		inherit.Label = me.Username + "'s default — " + myAddr
		options = append(options, inherit)
	default:
		options = append(options, inherit)
	}
	if hosted != "" {
		options = append(options, view.Option{Value: "hosted", Label: "SelfPost hosted (" + hosted + ")"})
	}
	// The mockup's order: whose default is followed now, hosted, "mine instead".
	if followsOther {
		options = append(options, inherit)
	}
	options = append(options, view.Option{Value: "none", Label: "No aggregate reports"}, view.Option{Value: "custom", Label: "Custom address"})

	switch {
	case followsOther:
		selected = "keep"
	case d.DMARCRuaUserID.Valid:
		selected = "inherit"
	case d.DMARCRua == "":
		selected = "none"
	case hosted != "" && strings.EqualFold(d.DMARCRua, hosted):
		selected = "hosted"
	default:
		selected, custom = "custom", d.DMARCRua
	}
	return options, selected, custom, help
}

// formatBound writes a multiplier bound the short way: "1.5", "5".
func formatBound(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

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

// blankAppForm is the application form as it opens: any address of the domain,
// no client IPs, the domain's limit.
func blankAppForm() view.OutAppForm {
	return view.OutAppForm{
		Mode: store.AddressModeWildcard, LimitMode: view.LimitDomain,
		Window: strconv.Itoa(defaultRateLimitWindowSeconds), Multiplier: formatMultiplier(store.DefaultAutoMultiplier),
	}
}

// submittedAppForm reads the application form as it was posted, to show it
// again with its refusal and what was typed.
func submittedAppForm(r *http.Request) view.OutAppForm {
	f := blankAppForm()
	f.Login = strings.TrimSpace(r.PostFormValue("login"))
	if mode := r.PostFormValue("mode"); mode != "" {
		f.Mode = mode
	}
	f.Addresses = r.PostFormValue("addresses")
	f.IPRestrict = r.PostFormValue("auth_ip_restrict") != ""
	f.AllowedIPs = r.PostFormValue("auth_allowed_ips")
	if mode := strings.TrimSpace(r.PostFormValue("rl_mode")); mode != "" {
		f.LimitMode = mode
	}
	f.MaxMessages = r.PostFormValue("max_messages")
	if window := r.PostFormValue("window_seconds"); window != "" {
		f.Window = window
	}
	if mult := r.PostFormValue("auto_multiplier"); mult != "" {
		f.Multiplier = mult
	}
	return f
}

// renderApplicationForm renders the application form: empty for a new
// application (a == nil), or the stored one. submitted, when set, replaces the
// fields with what was posted; formErr is the refusal shown above the form.
func (h *Handlers) renderApplicationForm(w http.ResponseWriter, r *http.Request, status int, d store.Domain,
	a *store.Application, submitted *view.OutAppForm, formErr string) {
	fail := func(what string, err error) {
		logf("panel: domain %d: application form: %s: %v", d.ID, what, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	retention := h.sendLogRetentionDays()
	domainRL, domainRLok, err := h.domains.RateLimit(d.ID)
	if err != nil {
		fail("domain rate limit", err)
		return
	}
	rate := view.AppRateLimit{
		L1Messages: h.l1Messages(), L1Window: h.l1Window(),
		MinMultiplier: formatBound(store.MinAutoMultiplier), MaxMultiplier: formatBound(store.MaxAutoMultiplier),
		DefaultMultiplier: formatBound(store.DefaultAutoMultiplier),
	}
	if domainRLok && domainRL.Active() {
		rate.DomainLimit = view.FormatRate(domainRL.MaxMessages, domainRL.WindowSeconds)
	}

	var (
		page  *view.OutApp
		form  = blankAppForm()
		state view.OutAppState
	)
	if a == nil {
		page = view.NewOutApp(h.shellMeta(r), d.ID, d.Name, "")
	} else {
		rl, ok, err := h.apps.RateLimit(a.ID)
		if err != nil {
			fail("rate limit", err)
			return
		}
		stats, err := h.store.AppSendStats(a.Login, retention, a.CreatedAt)
		if err != nil {
			fail("send stats", err)
			return
		}
		mult := rl.AutoMultiplier
		if mult <= 0 {
			mult = store.DefaultAutoMultiplier
		}
		form = view.OutAppForm{
			Login: a.Login, Mode: a.AddressMode, Addresses: strings.Join(a.Addresses, "\n"),
			IPRestrict: a.AuthIPRestrict, AllowedIPs: strings.Join(a.AuthAllowedIPs, "\n"),
			LimitMode: limitChoice(rl, ok), MaxMessages: intOrBlank(rl.MaxMessages), Window: windowOrDefault(rl.WindowSeconds),
			Multiplier: formatMultiplier(mult),
		}
		state = view.OutAppState{IPs: a.AuthIPRestrict, Limit: ok && rl.Active(), Auto: ok && rl.IsAuto()}
		rate.Peak = stats.PeakPerHour
		if state.Auto {
			rate.Computed, rate.Updated = intOrBlank(rl.MaxMessages), formatAutoUpdated(rl.AutoUpdatedAt)
		}
		page = view.NewOutApp(h.shellMeta(r), d.ID, d.Name, a.Login).
			WithApplication(d.ID, a.ID, stats.Total, stats.PeakPerHour, fmt.Sprintf("%.1f", stats.AvgPerHour), stats.WindowDays)
	}
	if submitted != nil {
		form = *submitted
		if a != nil {
			form.Login = a.Login // the login is not renamed by this form
		}
	}
	h.view.Render(w, status, "out-app", page.WithForm(form, state, rate).WithResult("", formErr))
}

// renderPasswordOnce answers with the page that shows a password a single time:
// the login and the password, which are not stored, and how to connect. The
// response is the one place the password exists, so it is never cached.
func (h *Handlers) renderPasswordOnce(w http.ResponseWriter, r *http.Request, status int, d store.Domain,
	a store.Application, password string, fresh bool) {
	senders := a.Addresses
	if a.AddressMode == store.AddressModeWildcard {
		senders = []string{"*@" + d.Name}
	}
	noStore(w)
	h.view.Render(w, status, "out-app-created", view.NewOutAppCreated(h.shellMeta(r), d.ID, d.Name, a.Login, password, fresh,
		h.cfg.Hostname, h.cfg.SubmissionEnabled, senders))
}

// HandleApplicationNew shows the form that adds an application.
func (h *Handlers) HandleApplicationNew(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	h.renderApplicationForm(w, r, http.StatusOK, d, nil, nil, "")
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
		h.renderApplicationForm(w, r, http.StatusBadRequest, d, nil, nil, "Invalid form submission.")
		return
	}
	submitted := submittedAppForm(r)
	set, err := h.parseApplicationForm(r)
	if err != nil {
		h.renderApplicationForm(w, r, http.StatusBadRequest, d, nil, &submitted, err.Error())
		return
	}
	a, password, err := h.apps.CreateWithSettings(d.ID, submitted.Login, set, h.recalcApp)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrLoginExists) {
			status = http.StatusConflict
		}
		h.renderApplicationForm(w, r, status, d, nil, &submitted, applicationErrorMessage(err))
		return
	}
	h.renderPasswordOnce(w, r, http.StatusCreated, d, a, password, true)
}

// HandleApplicationEdit shows an application's form.
func (h *Handlers) HandleApplicationEdit(w http.ResponseWriter, r *http.Request) {
	a, d, ok := h.lookupDomainApplication(w, r)
	if !ok {
		return
	}
	h.renderApplicationForm(w, r, http.StatusOK, d, &a, nil, "")
}

// HandleApplicationSave saves an application's form: sender, client IPs and
// rate limit in one step.
func (h *Handlers) HandleApplicationSave(w http.ResponseWriter, r *http.Request) {
	a, d, ok := h.lookupDomainApplication(w, r)
	if !ok {
		return
	}
	fail := func(msg string) {
		submitted := submittedAppForm(r)
		h.renderApplicationForm(w, r, http.StatusBadRequest, d, &a, &submitted, fmt.Sprintf("%s: %s", a.Login, msg))
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
	h.renderPasswordOnce(w, r, http.StatusOK, d, a, password, false)
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
