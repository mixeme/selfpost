package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

type dmarcListRow struct {
	store.DMARCReportSummary
	DomainID       int64
	ReceivedLabel  string
	PeriodLabel    string
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
	if p.IsGlobal() {
		return true
	}
	for _, id := range p.Domains {
		if id == d.ID {
			return true
		}
	}
	return false
}

// HandleDMARCList is the global DMARC reports index.
func (h *Handlers) HandleDMARCList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireDMARC(w, r)
	if !ok || !p.IsGlobal() {
		if ok && !p.IsGlobal() {
			http.NotFound(w, r)
		}
		return
	}
	assigned, err := h.assignedDomains(p)
	if err != nil {
		logf("panel: dmarc list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	names := make([]string, 0, len(assigned))
	domainIDs := make(map[string]int64, len(assigned))
	for _, d := range assigned {
		names = append(names, d.Name)
		domainIDs[d.Name] = d.ID
	}
	reports, err := h.store.ListDMARCReports(names, 100)
	if err != nil {
		logf("panel: dmarc list: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	stats, err := h.store.DMARCIngestStats()
	if err != nil {
		logf("panel: dmarc ingest stats: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]dmarcListRow, len(reports))
	for i, rep := range reports {
		rows[i] = dmarcListRow{
			DMARCReportSummary: rep,
			DomainID:           domainIDs[rep.Domain],
			ReceivedLabel:      rep.ReceivedAt.UTC().Format("2006-01-02 15:04"),
			PeriodLabel:        formatDMARCWindow(rep.PeriodBegin, rep.PeriodEnd),
		}
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — DMARC reports"
	data["Active"] = "dmarc"
	data["Reports"] = rows
	data["IngestStats"] = stats
	data["HostedAddress"] = h.dmarc.DefaultHostedSuggestion()
	data["RetentionMax"] = store.DMARCReportsMaxKeep
	data["RetentionDays"] = store.DMARCReportsMaxAgeDays
	if stats.LastReceivedAt != nil {
		data["LastReceivedLabel"] = stats.LastReceivedAt.UTC().Format("2006-01-02 15:04")
	}
	h.view.Render(w, http.StatusOK, "dmarc", data)
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
	reportRows := make([]dmarcListRow, len(reports))
	for i, rep := range reports {
		reportRows[i] = dmarcListRow{
			DMARCReportSummary: rep,
			ReceivedLabel:      rep.ReceivedAt.UTC().Format("2006-01-02 15:04"),
			PeriodLabel:        formatDMARCWindow(rep.PeriodBegin, rep.PeriodEnd),
		}
	}
	hints := make([]dmarc.SourceHint, len(sources))
	for i, s := range sources {
		hints[i] = dmarc.SourceHint{
			SourceIP:  s.SourceIP,
			PassCount: s.PassCount,
			FailCount: s.FailCount,
			ThisRelay: h.sourceIsThisRelay(s.SourceIP),
		}
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — " + d.Name + " DMARC"
	data["Active"] = "dmarc"
	data["Domain"] = d
	data["Reports"] = reportRows
	data["Pass7d"] = pass
	data["Fail7d"] = fail
	data["Sources"] = hints
	data["PolicyHint"] = dmarc.TightenPolicyHint(pass, fail, hints)
	data["WindowDays"] = windowDays
	h.view.Render(w, http.StatusOK, "dmarc_domain", data)
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
	data := h.pageBase(r)
	data["Title"] = fmt.Sprintf("SelfPost — %s report", rep.Reporter)
	data["Active"] = "dmarc"
	data["Report"] = rep
	data["Domain"] = d
	data["Hostname"] = h.cfg.Hostname
	data["WindowLabel"] = formatDMARCWindow(rep.PeriodBegin, rep.PeriodEnd)
	data["ReceivedLabel"] = rep.ReceivedAt.UTC().Format("2006-01-02 15:04")
	data["PeriodBeginLabel"] = rep.PeriodBegin.UTC().Format("2006-01-02 15:04")
	data["PeriodEndLabel"] = rep.PeriodEnd.UTC().Format("2006-01-02 15:04")
	h.view.Render(w, http.StatusOK, "dmarc_report", data)
}

func (h *Handlers) sourceIsThisRelay(ip string) bool {
	if ip == "" || h.dns == nil || h.cfg.Hostname == "" {
		return false
	}
	srv := h.dns.Server(h.cfg.Hostname, false)
	for _, s := range srv.IPs {
		if ip == s {
			return true
		}
	}
	return false
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
