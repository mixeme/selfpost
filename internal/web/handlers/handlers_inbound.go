package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/validate"
)

type inboundRow struct {
	store.InboundDomain
	DNS       health.Status
	Upstream  string
	TLSLabel  string
	RcptLabel string
}

func (h *Handlers) requireInbound(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if !h.cfg.InboundEnabled || h.inbound == nil {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	return h.requireGlobal(w, r)
}

func (h *Handlers) lookupInbound(w http.ResponseWriter, r *http.Request) (store.InboundDomain, bool) {
	if h.inbound == nil {
		http.NotFound(w, r)
		return store.InboundDomain{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return store.InboundDomain{}, false
	}
	d, err := h.inbound.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrInboundDomainNotFound) {
			http.NotFound(w, r)
			return store.InboundDomain{}, false
		}
		logf("panel: get inbound domain %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.InboundDomain{}, false
	}
	return d, true
}

// HandleInboundList is the inbound-relay domain list (global administrators only).
func (h *Handlers) HandleInboundList(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	h.renderInboundList(w, r, http.StatusOK, "", "")
}

func (h *Handlers) renderInboundList(w http.ResponseWriter, r *http.Request, status int, formErr, formName string) {
	list, err := h.inbound.List()
	if err != nil {
		logf("panel: inbound list: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — inbound"
	data["Active"] = "inbound"
	data["Domains"] = h.inboundRows(list)
	data["Error"] = formErr
	data["FormName"] = formName
	if r.URL.Query().Get("deleted") != "" {
		data["Flash"] = "Inbound domain deleted."
	}
	h.view.Render(w, status, "inbound", data)
}

func (h *Handlers) inboundRows(domains []store.InboundDomain) []inboundRow {
	rows := make([]inboundRow, len(domains))
	var wg sync.WaitGroup
	for i, d := range domains {
		rows[i] = inboundRow{
			InboundDomain: d,
			DNS:           health.StatusUnknown,
			Upstream:      inboundUpstream(d),
			TLSLabel:      tlsLabel(d.TLSMode),
			RcptLabel:     rcptLabel(d),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.dns != nil && h.cfg.Hostname != "" {
				rows[i].DNS = h.dns.InboundMX(d.Name, h.cfg.Hostname, false).Status
			}
		}()
	}
	wg.Wait()
	return rows
}

func inboundUpstream(d store.InboundDomain) string {
	if d.Host == "" {
		return "—"
	}
	return fmt.Sprintf("%s:%d", d.Host, d.Port)
}

func tlsLabel(mode string) string {
	switch mode {
	case store.TLSModeEncrypt:
		return "required"
	case store.TLSModeNone:
		return "off"
	default:
		return "opportunistic"
	}
}

func tlsStatusClass(mode string) string {
	if mode == store.TLSModeEncrypt {
		return "ok"
	}
	return "unknown"
}

func rcptLabel(d store.InboundDomain) string {
	if d.RecipientMode == store.RecipientModeAny {
		return "any"
	}
	n := d.RecipientCount
	if n == 1 {
		return "1 listed"
	}
	return fmt.Sprintf("%d listed", n)
}

// HandleAddInbound validates the name, creates the inbound domain, and
// redirects to its page.
func (h *Handlers) HandleAddInbound(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderInboundList(w, r, http.StatusBadRequest, "Invalid form submission.", "")
		return
	}
	raw := r.PostFormValue("name")
	name := validate.NormalizeDomain(raw)
	if err := validate.Domain(name); err != nil {
		h.renderInboundList(w, r, http.StatusBadRequest, err.Error(), raw)
		return
	}
	d, err := h.inbound.Add(name)
	if err != nil {
		if errors.Is(err, store.ErrInboundDomainExists) {
			h.renderInboundList(w, r, http.StatusConflict, "That inbound domain is already configured.", raw)
			return
		}
		logf("panel: add inbound domain %q: %v", name, err)
		h.renderInboundList(w, r, http.StatusInternalServerError,
			"Could not add the domain. Please check the logs and try again.", raw)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/%d", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	h.renderInboundDetail(w, r, http.StatusOK, d, inboundDetailView{})
}

type inboundDetailView struct {
	FormErr      string
	TransportErr string
	RecipientErr string
}

func (h *Handlers) renderInboundDetail(w http.ResponseWriter, r *http.Request, status int, d store.InboundDomain, extra inboundDetailView) {
	mx := dnscheck.Result{Status: health.StatusUnknown}
	if h.dns != nil && h.cfg.Hostname != "" {
		mx = h.dns.InboundMX(d.Name, h.cfg.Hostname, false)
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — " + d.Name
	data["Active"] = "inbound"
	data["Domain"] = d
	data["MX"] = mx
	data["MXValue"] = "10 " + strings.TrimSuffix(h.cfg.Hostname, ".") + "."
	data["Hostname"] = h.cfg.Hostname
	data["TLSLabel"] = tlsLabel(d.TLSMode)
	data["TLSClass"] = tlsStatusClass(d.TLSMode)
	data["RecipientText"] = strings.Join(d.Recipients, "\n")
	data["Flash"] = inboundFlash(r)
	data["FormErr"] = extra.FormErr
	data["TransportErr"] = extra.TransportErr
	data["RecipientErr"] = extra.RecipientErr
	h.view.Render(w, status, "inbound_domain", data)
}

func inboundFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Get("saved") != "":
		return "Upstream saved."
	case r.URL.Query().Get("recipients") != "":
		return "Recipients saved."
	case r.URL.Query().Get("rechecked") != "":
		return "DNS re-checked."
	default:
		return ""
	}
}

func (h *Handlers) HandleInboundDNSRecheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	if h.dns != nil && h.cfg.Hostname != "" {
		h.dns.InboundMX(d.Name, h.cfg.Hostname, true)
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/%d?rechecked=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundTransport(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{TransportErr: "Invalid form submission."})
		return
	}
	host := r.PostFormValue("host")
	port := r.PostFormValue("port")
	tlsMode := r.PostFormValue("tls_mode")
	if err := h.inbound.SetTransport(d.ID, host, port, tlsMode); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{TransportErr: err.Error()})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/%d?saved=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundRecipients(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{RecipientErr: "Invalid form submission."})
		return
	}
	mode := r.PostFormValue("recipient_mode")
	addrs := splitAddresses(r.PostFormValue("addresses"))
	if err := h.inbound.SetRecipients(d.ID, mode, addrs); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{RecipientErr: err.Error()})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/%d?recipients=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — delete " + d.Name
	data["Active"] = "inbound"
	data["Domain"] = d
	data["Upstream"] = inboundUpstream(d)
	h.view.Render(w, http.StatusOK, "inbound_delete", data)
}

func (h *Handlers) HandleInboundDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	if h.dns != nil {
		h.dns.Forget(d.Name)
	}
	if err := h.inbound.Delete(d.ID); err != nil {
		if errors.Is(err, store.ErrInboundDomainNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: delete inbound domain %d: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/inbound?deleted=1", http.StatusSeeOther)
}
