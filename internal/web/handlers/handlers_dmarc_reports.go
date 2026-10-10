package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

// dmarcRow is a report in a table of the pages.
func dmarcRow(rep store.DMARCReportSummary) view.DMARCReportRow {
	return view.DMARCReportRow{
		Received: view.FormatReceived(rep.ReceivedAt), Domain: rep.Domain, Reporter: rep.Reporter,
		Window: formatDMARCWindow(rep.PeriodBegin, rep.PeriodEnd), Pass: rep.PassCount, Fail: rep.FailCount,
		Href: view.DMARCReportHref(rep.ID),
	}
}

func (h *Handlers) requireDMARC(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if !h.cfg.DMARCEnabled || h.dmarc == nil {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	p, ok := h.principal(r)
	if !ok {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	return p, true
}

func (h *Handlers) canViewDMARCDomain(p auth.Principal, d store.Domain) bool {
	return p.CanAccessDomain(d.ID)
}

// HandleDMARCList is the DMARC reports hub. The global role sees every report
// and the ingest statistics of the instance; a domain administrator sees the
// reports of the sending domains assigned to them, and no ingest statistics
// (the box describes the whole instance: its mailbox, its failures).
func (h *Handlers) HandleDMARCList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireDMARC(w, r)
	if !ok {
		return
	}
	assigned, err := h.assignedDomains(p)
	if err != nil {
		logf("panel: dmarc list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	scope := store.DMARCReportScope{AllDomains: p.IsGlobal()}
	domainIDs := make(map[string]int64, len(assigned))
	for _, d := range assigned {
		scope.Domains = append(scope.Domains, d.Name)
		domainIDs[d.Name] = d.ID
	}
	reports, err := h.store.ListDMARCReports(scope, 100)
	if err != nil {
		logf("panel: dmarc list: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]view.DMARCReportRow, len(reports))
	for i, rep := range reports {
		rows[i] = dmarcRow(rep)
		if id := domainIDs[rep.Domain]; id != 0 {
			rows[i].DomainHref = view.DMARCDomainHref(id)
		}
	}
	if !p.IsGlobal() {
		page := view.NewDMARCHub(h.shellMeta(r), view.IngestInput{}).WithoutIngest().WithReports(rows)
		h.view.Render(w, http.StatusOK, "dmarc", page)
		return
	}
	stats, err := h.store.DMARCIngestStats()
	if err != nil {
		logf("panel: dmarc ingest stats: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	in := view.IngestInput{
		OK: stats.IngestOK, KeptThisWeek: stats.KeptThisWeek, ParseFailures: stats.ParseFailures,
		Hosted: h.dmarc.DefaultHostedSuggestion(), Host: h.cfg.Hostname,
		RetentionMax: store.DMARCReportsMaxKeep, RetentionDays: store.DMARCReportsMaxAgeDays,
	}
	if stats.LastReceivedAt != nil {
		in.Last = *stats.LastReceivedAt
	}
	h.view.Render(w, http.StatusOK, "dmarc", view.NewDMARCHub(h.shellMeta(r), in).WithReports(rows))
}

// HandleDMARCDomain shows roll-ups for one sending domain.
func (h *Handlers) HandleDMARCDomain(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDMARC(w, r); !ok {
		return
	}
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	p, _ := h.principal(r)
	if !h.canViewDMARCDomain(p, d) {
		http.NotFound(w, r)
		return
	}
	const windowDays = 7
	pass, fail, err := h.store.DMARCDomainRollup(d.Name, windowDays)
	if err != nil {
		logf("panel: dmarc domain rollup %s: %v", d.Name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sources, err := h.store.DMARCSourceRollups(d.Name, windowDays)
	if err != nil {
		logf("panel: dmarc source rollups %s: %v", d.Name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	reports, err := h.store.ListDMARCReportsForDomain(d.Name, 50)
	if err != nil {
		logf("panel: dmarc domain reports %s: %v", d.Name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	reportRows := make([]view.DMARCReportRow, len(reports))
	for i, rep := range reports {
		reportRows[i] = dmarcRow(rep)
	}
	relay := h.relayIPs()
	hints := make([]dmarc.SourceHint, len(sources))
	sourceRows := make([]view.DMARCSourceRow, len(sources))
	for i, s := range sources {
		hints[i] = dmarc.SourceHint{SourceIP: s.SourceIP, PassCount: s.PassCount, FailCount: s.FailCount, ThisRelay: relay[s.SourceIP]}
		sourceRows[i] = view.DMARCSourceRow{
			Source: s.SourceIP, ThisRelay: relay[s.SourceIP], Pass: s.PassCount, Fail: s.FailCount, Disposition: s.Disposition,
		}
	}
	page := view.NewDMARCDomain(h.shellMeta(r), d.ID, d.Name, true, pass, fail, windowDays,
		dmarc.TightenPolicyHint(pass, fail, hints)).
		WithSources(sourceRows).WithReports(reportRows)
	h.view.Render(w, http.StatusOK, "dmarc-domain", page)
}

// HandleDMARCReport shows one parsed aggregate report.
func (h *Handlers) HandleDMARCReport(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDMARC(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	rep, err := h.store.GetDMARCReport(id)
	if errors.Is(err, store.ErrDMARCReportNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		logf("panel: dmarc report %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p, _ := h.principal(r)
	domains, err := h.assignedDomains(p)
	if err != nil {
		logf("panel: dmarc report authz: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var d store.Domain
	found := false
	for _, cand := range domains {
		if cand.Name == rep.Domain {
			d = cand
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	relay := h.relayIPs()
	records := make([]view.DMARCRecordRow, len(rep.Records))
	for i, rec := range rep.Records {
		records[i] = view.DMARCRecordRow{
			Source: rec.SourceIP, ThisRelay: relay[rec.SourceIP], Count: rec.Count, Disposition: rec.Disposition,
			SPF: rec.SPFResult, DKIM: rec.DKIMResult, HeaderFrom: rec.HeaderFrom,
		}
	}
	const stamp = "2 Jan 15:04"
	page := view.NewDMARCReport(h.shellMeta(r), view.ReportInput{
		ID: rep.ID, Reporter: rep.Reporter, ReportID: rep.ReportID, Domain: rep.Domain, DomainID: d.ID,
		Contact: rep.ContactEmail, Pass: rep.PassCount, Fail: rep.FailCount,
		Window:   formatDMARCWindow(rep.PeriodBegin, rep.PeriodEnd),
		Period:   rep.PeriodBegin.UTC().Format(stamp) + " – " + rep.PeriodEnd.UTC().Format(stamp) + " UTC",
		Received: rep.ReceivedAt.UTC().Format("2006-01-02 15:04") + " UTC",
		PolicyP:  rep.PolicyP, PolicySP: rep.PolicySP, PolicyPct: rep.PolicyPct,
		PolicyADKIM: rep.PolicyADKIM, PolicyASPF: rep.PolicyASPF, Recipient: rep.Recipient,
		Hub: true,
	}).WithRecords(records)
	h.view.Render(w, http.StatusOK, "dmarc-report", page)
}

// relayIPs is the set of addresses this server sends from, so that a source in
// a report can be told to be this relay. Empty while the server's name or its
// addresses are unknown.
func (h *Handlers) relayIPs() map[string]bool {
	if h.dns == nil || h.cfg.Hostname == "" {
		return nil
	}
	ips := map[string]bool{}
	for _, ip := range h.dns.Server(h.cfg.Hostname, false).IPs {
		ips[ip] = true
	}
	return ips
}

func formatDMARCWindow(begin, end time.Time) string {
	if begin.IsZero() {
		return ""
	}
	if begin.Year() == end.Year() && begin.YearDay() == end.YearDay() {
		return begin.UTC().Format("2 Jan")
	}
	return begin.UTC().Format("2 Jan") + " – " + end.UTC().Format("2 Jan")
}
