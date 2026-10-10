// Package web implements the SelfPost control panel's HTTP surface: the
// one-time administrator setup flow (security.md), login/session handling
// (security.md) and the authenticated shell the later phases build on.
package web

import (
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/legal"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/handlers"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Config holds the panel's HTTP-facing configuration.
type Config struct {
	// Hostname is the server's external hostname, used to build the absolute
	// setup link shown in the logs (security.md; guide § Environment
	// variables for SELFPOST_HOSTNAME).
	Hostname string
	// CookieSecure sets the Secure attribute on the session cookie. It defaults
	// to true (security.md); it exists as a knob only so the panel can be tested
	// over plain HTTP in development, never for production.
	CookieSecure bool
	// SubmissionEnabled mirrors SUBMISSION_ENABLE: whether this deployment also
	// runs the 587/STARTTLS submission listener next to the primary 465 one
	// (architecture.md § Mail path). The panel only reports it on the domain
	// page's connection settings; it is a deploy-time flag, not something the
	// panel can verify.
	SubmissionEnabled bool
	// MailLogPath is where Postfix's delivery log lives, read by the mail.log
	// monitoring view (architecture.md § Panel HTTP surface). It is the same path
	// the log-tailer role follows in cmd/panel.
	MailLogPath string
	// DataDir and DBPath locate the persistent state a full backup archives
	// (architecture.md § Persistence); DeployRoot is the operator project
	// directory (docker-compose.yml, .env, certs/); Version is stamped into
	// the backup manifest. They mirror the panel's own configuration.
	DataDir    string
	DBPath     string
	DeployRoot string
	Version    string
	// TrustedProxyCIDRs are the reverse-proxy addresses allowed to supply
	// X-Forwarded-For (env TRUSTED_PROXY_CIDR). A request whose
	// direct peer (RemoteAddr) is not in this list never has its XFF header
	// honoured, so the header can't be spoofed by anyone but a trusted proxy.
	// Empty (the default) keeps rate-limiting keyed on RemoteAddr only.
	TrustedProxyCIDRs []*net.IPNet
	// TLSCertFile is the certificate Postfix serves on 465/587 (guide §
	// Environment variables), read read-only by the status page to report how
	// much validity is left.
	TLSCertFile string
	// OpenDKIMSocket and JournalSocket are the two milter sockets Postfix
	// connects to. The status page stats them: the first is required for mail
	// to leave at all (OpenDKIM runs with default_action=tempfail), the second
	// only for the send log (the journal-milter fails open).
	OpenDKIMSocket string
	JournalSocket  string
	// SessionIdleDays is the sliding inactivity window after which a login
	// session expires (env PANEL_SESSION_IDLE_DAYS, plan B.1). Non-positive
	// falls back to the 7-day default.
	SessionIdleDays int
	// DNSResolvers are the recursive resolvers the deliverability checks query
	// (env SELFPOST_DNS_RESOLVERS). Empty uses dnscheck.DefaultResolvers. The
	// checks must not go through the system resolver — see dnscheck's
	// externalResolver — so this is how a closed network points them at its own.
	DNSResolvers []string
	// RateLimitMessagesPerIP and RateLimitWindowSeconds are the level-1
	// Postfix anvil backstop (env RATE_LIMIT_*), mirrored into the panel for
	// display and to cap domain/app level-2 ceilings (guide § Rate limiting).
	RateLimitMessagesPerIP int
	RateLimitWindowSeconds int
	// RetryPolicy is this Postfix's deferred-mail timings, snapshotted once
	// when the HTTP role starts. Handlers read the cache; they never call
	// postconf (architecture.md).
	RetryPolicy postfix.RetryPolicy
	// InboundEnabled mirrors INBOUND_RELAY_ENABLE.
	InboundEnabled bool
	// InboundAntispamMilter mirrors INBOUND_ANTISPAM_MILTER (inet:host:port or
	// unix:/path) and InboundAntispamAction INBOUND_ANTISPAM_MILTER_ACTION
	// ("accept" or "tempfail", "" when the variable holds anything else): the
	// panel only reports them.
	InboundAntispamMilter string
	InboundAntispamAction string
	// DMARCEnabled mirrors DMARC_REPORTS_ENABLE.
	DMARCEnabled bool
	// SendLogRetentionEnvDefault is SEND_LOG_RETENTION_DAYS at panel start.
	SendLogRetentionEnvDefault int
}

// Server is the panel HTTP application.
type Server struct {
	cfg      Config
	auth     *auth.Module
	handlers *handlers.Handlers
}

// New builds the panel server. setupTokenPath is where the current setup token
// is mirrored on disk (security.md); domains is the sending-domain service
// that owns DKIM keys and the OpenDKIM tables (architecture.md § OpenDKIM);
// apps owns application SASL accounts and the Postfix sender map
// (architecture.md § Mail path).
func New(st *store.Store, domains *domain.Service, apps *app.Service, inboundSvc *inbound.Service, dmarcSvc *dmarc.Service, cfg Config, setupTokenPath string) (*Server, error) {
	v, err := view.New(cfg.Version)
	if err != nil {
		return nil, err
	}
	v.SetInboundEnabled(cfg.InboundEnabled)
	v.SetDMARCEnabled(cfg.DMARCEnabled)
	a := auth.New(st, auth.Config{
		CookieSecure:      cfg.CookieSecure,
		Hostname:          cfg.Hostname,
		SessionIdleDays:   cfg.SessionIdleDays,
		TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
	}, v, setupTokenPath)
	h := handlers.New(st, domains, apps, inboundSvc, dmarcSvc, handlers.Config{
		Hostname:                   cfg.Hostname,
		SubmissionEnabled:          cfg.SubmissionEnabled,
		MailLogPath:                cfg.MailLogPath,
		DataDir:                    cfg.DataDir,
		DBPath:                     cfg.DBPath,
		DeployRoot:                 cfg.DeployRoot,
		Version:                    cfg.Version,
		TLSCertFile:                cfg.TLSCertFile,
		OpenDKIMSocket:             cfg.OpenDKIMSocket,
		JournalSocket:              cfg.JournalSocket,
		RateLimitMessagesPerIP:     cfg.RateLimitMessagesPerIP,
		RateLimitWindowSeconds:     cfg.RateLimitWindowSeconds,
		RetryPolicy:                cfg.RetryPolicy,
		InboundEnabled:             cfg.InboundEnabled,
		InboundAntispamMilter:      cfg.InboundAntispamMilter,
		InboundAntispamAction:      cfg.InboundAntispamAction,
		DMARCEnabled:               cfg.DMARCEnabled,
		SendLogRetentionEnvDefault: cfg.SendLogRetentionEnvDefault,
	}, v, dnscheck.New(cfg.DNSResolvers), &health.MachineSampler{}, a)
	return &Server{cfg: cfg, auth: a, handlers: h}, nil
}

// Start performs first-run bootstrapping: if there is no administrator yet, it
// generates and announces the setup link (security.md). Safe to call once at
// server startup.
func (s *Server) Start() error {
	return s.auth.Bootstrap()
}

// Handler returns the panel's HTTP handler (router).
func (s *Server) Handler() http.Handler {
	mux, authed := s.muxes()
	mux.Handle("/", s.auth.RequireAuth(authed))
	return s.secure(mux)
}

// muxes registers every route: the public ones, and the ones behind a session.
// Both muxes are flat — one pattern per route, nothing mounted as a sub-router —
// and building them only takes method values, it never calls into s.auth or
// s.handlers: the route guard test (guard_routes_test.go) runs this on a bare
// Server and asks each mux which pattern answers a path.
func (s *Server) muxes() (public, authed *http.ServeMux) {
	mux := http.NewServeMux()
	h := s.handlers

	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/license", handleLicense)
	mux.Handle("/static/", view.StaticHandler())
	mux.HandleFunc("/setup/", s.auth.HandleSetup)
	mux.HandleFunc("/login", s.auth.HandleLogin)
	mux.HandleFunc("/logout", s.auth.HandleLogout)

	authed = http.NewServeMux()
	// A URL reads like the menu: /<group>/<page>, an entity under its list, an
	// action under the thing it changes (docs/plans/panel-redesign.md §
	// Routes). The pre-2.0 paths are gone, not redirected.
	authed.HandleFunc("GET /{$}", redirectHome)
	authed.HandleFunc("GET /help", h.HandleHelp)

	// Account: every signed-in user's own.
	authed.HandleFunc("GET /account", h.HandleAccount)
	authed.HandleFunc("POST /account/profile", h.HandleAccountProfile)
	authed.HandleFunc("POST /account/password", h.HandleAccountPassword)

	// Overview. Its handler checks the role itself: the page is global-only
	// but does not live under /server/.
	authed.HandleFunc("GET /overview", h.HandleOverview)
	authed.HandleFunc("GET /overview/fragment", h.HandleOverviewFragment)

	// Outbound: checked per domain in the handlers (lookupDomain,
	// lookupApplication), adding and deleting by role.
	authed.HandleFunc("GET /outbound/domains", h.HandleDashboard)
	authed.HandleFunc("POST /outbound/domains", h.HandleAddDomain)
	authed.HandleFunc("GET /outbound/domains/{id}", h.HandleDomainDetail)
	authed.HandleFunc("POST /outbound/domains/{id}/dns-recheck", h.HandleDomainDNSRecheck)
	authed.HandleFunc("GET /outbound/domains/{id}/delete", h.HandleDeleteConfirm)
	authed.HandleFunc("POST /outbound/domains/{id}/delete", h.HandleDeleteDomain)
	authed.HandleFunc("GET /outbound/domains/{id}/settings", h.HandleDomainSettings)
	authed.HandleFunc("POST /outbound/domains/{id}/settings/reports", h.HandleDomainDMARC)
	authed.HandleFunc("POST /outbound/domains/{id}/settings/ratelimit", h.HandleDomainRateLimit)
	authed.HandleFunc("POST /outbound/domains/{id}/settings/ratelimit/recalc", h.HandleDomainRateLimitRecalc)
	authed.HandleFunc("POST /outbound/domains/{id}/settings/export", h.HandleExportDomain)
	// An application lives under its domain, and its form is one POST: sender,
	// client IPs and rate limit are saved together. The page that shows a new
	// password is the response to its POST and has no GET path.
	authed.HandleFunc("GET /outbound/domains/{id}/applications/new", h.HandleApplicationNew)
	authed.HandleFunc("POST /outbound/domains/{id}/applications/new", h.HandleApplicationCreate)
	authed.HandleFunc("GET /outbound/domains/{id}/applications/{aid}", h.HandleApplicationEdit)
	authed.HandleFunc("POST /outbound/domains/{id}/applications/{aid}", h.HandleApplicationSave)
	authed.HandleFunc("POST /outbound/domains/{id}/applications/{aid}/password", h.HandleRegenPassword)
	authed.HandleFunc("POST /outbound/domains/{id}/applications/{aid}/ratelimit/recalc", h.HandleAppRateLimitRecalc)
	authed.HandleFunc("POST /outbound/domains/{id}/applications/{aid}/delete", h.HandleDeleteApplication)

	authed.HandleFunc("GET /outbound/log", h.HandleDeliveries)
	authed.HandleFunc("GET /outbound/log/fragment", h.HandleDeliveriesRows)
	authed.HandleFunc("GET /outbound/log/{id}", h.HandleDelivery)
	authed.HandleFunc("GET /outbound/queue", h.HandleMailQueue)
	authed.HandleFunc("GET /outbound/queue/fragment", h.HandleMailQueueBody)

	if s.cfg.DMARCEnabled {
		authed.HandleFunc("GET /outbound/dmarc", h.HandleDMARCList)
		authed.HandleFunc("GET /outbound/dmarc/reports/{id}", h.HandleDMARCReport)
		authed.HandleFunc("GET /outbound/dmarc/domains/{id}", h.HandleDMARCDomain)
	}

	// Inbound: checked per inbound domain (requireInboundDomain).
	if s.cfg.InboundEnabled {
		authed.HandleFunc("GET /inbound/domains", h.HandleInboundList)
		authed.HandleFunc("POST /inbound/domains", h.HandleAddInbound)
		authed.HandleFunc("GET /inbound/domains/{id}", h.HandleInboundDetail)
		authed.HandleFunc("POST /inbound/domains/{id}/dns-recheck", h.HandleInboundDNSRecheck)
		authed.HandleFunc("POST /inbound/domains/{id}/upstream", h.HandleInboundTransport)
		authed.HandleFunc("POST /inbound/domains/{id}/recipients", h.HandleInboundRecipients)
		authed.HandleFunc("GET /inbound/domains/{id}/delete", h.HandleInboundDeleteConfirm)
		authed.HandleFunc("POST /inbound/domains/{id}/delete", h.HandleInboundDelete)
	}

	// Server: everything under /server/ is for the global role, so the subtree
	// is guarded once, here, by server() — a route added to this group cannot
	// forget the check. The handlers keep their own requireGlobal as well.
	server := func(pattern string, handler http.HandlerFunc) {
		if _, path, _ := strings.Cut(pattern, " "); !strings.HasPrefix(path, "/server/") {
			panic("web: server() registers routes under /server/ only: " + pattern)
		}
		authed.HandleFunc(pattern, globalOnly(handler))
	}
	server("GET /server/health", h.HandleHealth)
	server("GET /server/health/fragment", h.HandleHealthFragment)
	server("POST /server/health/recheck", h.HandleHealthRecheck)
	server("POST /server/health/reload", h.HandleReload)
	server("GET /server/log", h.HandleSystemLog)
	server("GET /server/log/fragment", h.HandleSystemLogBody)
	server("GET /server/backup", h.HandleBackupPage)
	server("POST /server/backup", h.HandleBackup)
	server("POST /server/backup/import", h.HandleImportDomain)
	server("GET /server/users", h.HandleUsers)
	server("GET /server/users/new", h.HandleUserNew)
	server("POST /server/users/new", h.HandleUserNew)
	server("GET /server/users/{uid}", h.HandleUserEdit)
	server("POST /server/users/{uid}", h.HandleUserEdit)
	server("GET /server/users/{uid}/delete", h.HandleUserDeleteConfirm)
	server("POST /server/users/{uid}/delete", h.HandleUserDelete)
	server("GET /server/settings", h.HandleServerSettings)
	server("POST /server/settings", h.HandleServerSettings)
	server("GET /server/components", h.HandleComponents)

	return mux, authed
}

// globalOnly answers 404 to anyone but the global role before the wrapped
// handler runs — the same answer a missing page gets, so the panel does not
// confirm to a domain administrator that the Server pages exist.
func globalOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PrincipalFromRequest(r)
		if !ok || !p.IsGlobal() {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

func redirectHome(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromRequest(r)
	if ok && !p.IsGlobal() {
		// A domain administrator lands on what they reach: their sending
		// domains, or the inbound ones when that is all they are assigned.
		if !p.HasOutbound() && p.HasInbound() {
			http.Redirect(w, r, "/inbound/domains", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/outbound/domains", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/overview", http.StatusSeeOther)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := health.Liveness(); err != nil {
		http.Error(w, "unhealthy\n", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func handleLicense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(legal.License)
}

func logf(format string, args ...any) {
	log.Printf(format, args...)
}
