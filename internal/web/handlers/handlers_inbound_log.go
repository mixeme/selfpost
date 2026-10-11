package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Inbound › Log and Inbound › Filter lists. The log is for everyone who reaches
// an inbound domain, with the rows of their domains only (requireInbound); the
// lists are instance-wide and so the global role's (requireInboundGlobal), and
// answer 404 to anyone else, like the rest of what only that role may do.

const inboundLogPageSize = 50

// logDay is the form of a day in the log's date filters and in <input
// type="date">.
const logDay = "2006-01-02"

// HandleInboundLog renders the Inbound log: the journal of port 25's decisions,
// filtered server-side by domain, decision and day, a page at a time. A filter
// value that is not one the page offers is dropped rather than refused, and the
// domains a domain administrator may read are decided here, before the query
// runs - the dropdown only offers them.
func (h *Handlers) HandleInboundLog(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireInbound(w, r)
	if !ok {
		return
	}
	assigned, err := h.assignedInboundDomains(p)
	if err != nil {
		logf("panel: inbound log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	names := make([]string, 0, len(assigned))
	offered := make(map[string]bool, len(assigned))
	for _, d := range assigned {
		names = append(names, d.Name)
		offered[d.Name] = true
	}

	q := r.URL.Query()
	form := view.InLogFilter{}
	query := store.InboundSpamFilter{}
	if d := q.Get("domain"); offered[d] {
		form.Domain, query.Domain = d, d
	}
	switch d := q.Get("decision"); d {
	case store.SpamDecisionAccept, store.SpamDecisionReject, store.SpamDecisionTempfail, store.SpamDecisionQuarantine:
		form.Decision, query.Decision = d, d
	}
	if t, err := time.Parse(logDay, q.Get("from")); err == nil {
		form.From, query.Since = t.Format(logDay), t
	}
	if t, err := time.Parse(logDay, q.Get("to")); err == nil {
		form.To, query.Until = t.Format(logDay), t
	}
	// A global administrator reads the whole journal, including rows left
	// behind by a domain that has since been deleted; anyone else only what is
	// assigned to them, however empty that is.
	if !p.IsGlobal() {
		query.Domains = names
	}

	total, err := h.store.CountInboundSpam(query)
	if err != nil {
		logf("panel: inbound log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	lastPage := 1
	if total > 0 {
		lastPage = int((total + inboundLogPageSize - 1) / inboundLogPageSize)
	}
	page := min(parsePage(q.Get("p")), lastPage)
	rows, err := h.store.QueryInboundSpam(query, inboundLogPageSize, (page-1)*inboundLogPageSize)
	if err != nil {
		logf("panel: inbound log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	out := view.NewInLog(h.shellMeta(r), h.inboundSpamLogRetentionDays(), names, form)
	table := make([]view.InLogRow, len(rows))
	for i, row := range rows {
		table[i] = out.NewRow(view.InLogRowInput{
			ID: row.ID, At: row.CreatedAt, From: row.From, Client: row.ClientIP, To: row.To, Subject: row.Subject,
			Decision: row.Decision, Engine: row.Engine, Detail: row.Detail,
		})
	}
	h.view.Render(w, http.StatusOK, "in-log", out.WithRows(table, page, lastPage))
}

// inboundSpamLogRetentionDays returns the effective inbound journal retention
// window.
func (h *Handlers) inboundSpamLogRetentionDays() int {
	days, err := h.store.GetInboundSpamLogRetentionDays(h.cfg.InboundSpamLogRetentionEnvDefault)
	if err != nil {
		logf("panel: inbound log retention: %v", err)
		if err := store.ValidateInboundSpamLogRetentionDays(h.cfg.InboundSpamLogRetentionEnvDefault); err == nil {
			return h.cfg.InboundSpamLogRetentionEnvDefault
		}
		return store.InboundSpamLogRetentionDaysDefault
	}
	return days
}

// ---- Filter lists

func (h *Handlers) requireFilterLists(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := h.requireInboundGlobal(w, r); !ok {
		return false
	}
	if h.lists == nil {
		http.NotFound(w, r)
		return false
	}
	return true
}

// filterListForm is what the add form shows: the list and kind it offers (only
// values it knows are kept) and the text of the two fields.
type filterListForm struct {
	list, kind, value, note string
}

// HandleFilterLists shows the lists. The query fills the add form in - the
// Inbound log's Allow / Deny sender links lead here - and only a list and a
// kind the form offers are taken; the value is just put in the field.
func (h *Handlers) HandleFilterLists(w http.ResponseWriter, r *http.Request) {
	if !h.requireFilterLists(w, r) {
		return
	}
	q := r.URL.Query()
	form := filterListForm{value: q.Get("value")}
	switch l := q.Get("list"); l {
	case store.SpamListAllow, store.SpamListDeny:
		form.list = l
	}
	switch k := q.Get("kind"); k {
	case store.SpamEntryAddress, store.SpamEntryDomain, store.SpamEntryIP:
		form.kind = k
	}
	h.renderFilterLists(w, r, http.StatusOK, filterListsFlash(r), "", form)
}

func filterListsFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Has("added"):
		return "Entry added."
	case r.URL.Query().Has("removed"):
		return "Entry removed."
	}
	return ""
}

func (h *Handlers) renderFilterLists(w http.ResponseWriter, r *http.Request, status int, flash, formErr string, form filterListForm) {
	entries, err := h.lists.Entries()
	if err != nil {
		logf("panel: filter lists: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	on, _, _ := h.inboundFilter()
	in := make([]view.FilterEntryInput, len(entries))
	for i, e := range entries {
		in[i] = view.FilterEntryInput{ID: e.ID, List: e.List, Kind: e.Kind, Value: e.Value, Note: e.Note, Added: e.CreatedAt}
	}
	page := view.NewFilterLists(h.shellMeta(r), view.FilterListsInput{FilterOn: on, LastWritten: h.lists.LastWritten()}).
		WithEntries(in).
		WithResult(flash, formErr)
	// A blank list or kind leaves the form on its first choice.
	if form.list == "" {
		form.list = store.SpamListDeny
	}
	if form.kind == "" {
		form.kind = store.SpamEntryAddress
	}
	h.view.Render(w, status, "in-filter-lists", page.WithForm(form.list, form.kind, form.value, form.note))
}

// HandleFilterListAdd adds an entry and rewrites the map files. A refusal - an
// entry the lists do not take, or one already on the list - shows the page again
// with the message and what was typed.
func (h *Handlers) HandleFilterListAdd(w http.ResponseWriter, r *http.Request) {
	if !h.requireFilterLists(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderFilterLists(w, r, http.StatusBadRequest, "", "Invalid form submission.", filterListForm{})
		return
	}
	form := filterListForm{
		list: r.PostFormValue("list_type"), kind: r.PostFormValue("entry_type"),
		value: r.PostFormValue("value"), note: r.PostFormValue("note"),
	}
	_, err := h.lists.Add(form.list, form.kind, form.value, form.note)
	var refused *inbound.ValidationError
	switch {
	case err == nil:
		http.Redirect(w, r, "/inbound/filter-lists?added=1", http.StatusSeeOther)
	case errors.Is(err, store.ErrSpamListEntryExists):
		h.renderFilterLists(w, r, http.StatusUnprocessableEntity, "", "That entry is already on the list.", form)
	case errors.As(err, &refused):
		h.renderFilterLists(w, r, http.StatusUnprocessableEntity, "", refused.Error(), form)
	default:
		logf("panel: add filter list entry: %v", err)
		h.renderFilterLists(w, r, http.StatusInternalServerError, "", "Could not add the entry. Check the logs and try again.", form)
	}
}

// HandleFilterListDelete removes an entry and rewrites the map files.
func (h *Handlers) HandleFilterListDelete(w http.ResponseWriter, r *http.Request) {
	if !h.requireFilterLists(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := h.lists.Delete(id); err != nil {
		if errors.Is(err, store.ErrSpamListEntryNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: delete filter list entry %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/inbound/filter-lists?removed=1", http.StatusSeeOther)
}
