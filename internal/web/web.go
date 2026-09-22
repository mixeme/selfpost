// Package web implements the SelfPost control panel's HTTP surface: the
// one-time administrator setup flow (security.md), login/session handling
// (security.md) and the authenticated shell the later phases build on.
package web

import (
	"log"
	"net"
	"net/http"

	"github.com/mixeme/selfpost/internal/app"
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
func New(st *store.Store, domains *domain.Service, apps *app.Service, inboundSvc *inbound.Service, cfg Config, setupTokenPath string) (*Server, error) {
	v, err := view.New(cfg.Version)
	if err != nil {
		return nil, err
	}
	v.SetInboundEnabled(cfg.InboundEnabled)
	a := auth.New(st, auth.Config{
		CookieSecure:      cfg.CookieSecure,
		Hostname:          cfg.Hostname,
		SessionIdleDays:   cfg.SessionIdleDays,
		TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
	}, v, setupTokenPath)
	h := handlers.New(st, domains, apps, inboundSvc, handlers.Config{
		Hostname:               cfg.Hostname,
		SubmissionEnabled:      cfg.SubmissionEnabled,
		MailLogPath:            cfg.MailLogPath,
		DataDir:                cfg.DataDir,
		DBPath:                 cfg.DBPath,
		DeployRoot:             cfg.DeployRoot,
		Version:                cfg.Version,
		TLSCertFile:            cfg.TLSCertFile,
		OpenDKIMSocket:         cfg.OpenDKIMSocket,
		JournalSocket:          cfg.JournalSocket,
		RateLimitMessagesPerIP: cfg.RateLimitMessagesPerIP,
		RateLimitWindowSeconds: cfg.RateLimitWindowSeconds,
		RetryPolicy:            cfg.RetryPolicy,
		InboundEnabled:         cfg.InboundEnabled,
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
	mux := http.NewServeMux()
	h := s.handlers

	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/license", handleLicense)
	mux.Handle("/static/", view.StaticHandler())
	mux.HandleFunc("/setup/", s.auth.HandleSetup)
	mux.HandleFunc("/login", s.auth.HandleLogin)
	mux.HandleFunc("/logout", s.auth.HandleLogout)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /{$}", redirectHome)
	authed.HandleFunc("GET /status", h.HandleStatus)
	authed.HandleFunc("GET /status/fragment", h.HandleStatusFragment)
	authed.HandleFunc("POST /status/recheck", h.HandleStatusRecheck)

	authed.HandleFunc("GET /domains", h.HandleDashboard)
	authed.HandleFunc("POST /domains", h.HandleAddDomain)
	authed.HandleFunc("POST /domains/import", h.HandleImportDomain)
	authed.HandleFunc("GET /domains/{id}", h.HandleDomainDetail)
	authed.HandleFunc("POST /domains/{id}/dns-recheck", h.HandleDomainDNSRecheck)
	authed.HandleFunc("GET /domains/{id}/delete", h.HandleDeleteConfirm)
	authed.HandleFunc("POST /domains/{id}/delete", h.HandleDeleteDomain)
	authed.HandleFunc("POST /domains/{id}/applications", h.HandleAddApplication)
	authed.HandleFunc("POST /domains/{id}/ratelimit", h.HandleDomainRateLimit)
	authed.HandleFunc("POST /domains/{id}/dmarc", h.HandleDomainDMARC)
	authed.HandleFunc("POST /domains/{id}/export", h.HandleExportDomain)
	authed.HandleFunc("POST /applications/{aid}/mode", h.HandleUpdateAppMode)
	authed.HandleFunc("POST /applications/{aid}/password", h.HandleRegenPassword)
	authed.HandleFunc("POST /applications/{aid}/ratelimit", h.HandleAppRateLimit)
	authed.HandleFunc("POST /applications/{aid}/delete", h.HandleDeleteApplication)
	authed.HandleFunc("POST /reload", h.HandleReload)

	if s.cfg.InboundEnabled {
		authed.HandleFunc("GET /inbound", h.HandleInboundList)
		authed.HandleFunc("POST /inbound", h.HandleAddInbound)
		authed.HandleFunc("GET /inbound/{id}", h.HandleInboundDetail)
		authed.HandleFunc("POST /inbound/{id}/dns-recheck", h.HandleInboundDNSRecheck)
		authed.HandleFunc("POST /inbound/{id}/upstream", h.HandleInboundTransport)
		authed.HandleFunc("POST /inbound/{id}/recipients", h.HandleInboundRecipients)
		authed.HandleFunc("GET /inbound/{id}/delete", h.HandleInboundDeleteConfirm)
		authed.HandleFunc("POST /inbound/{id}/delete", h.HandleInboundDelete)
	}

	authed.HandleFunc("/settings", h.HandleSettings)
	authed.HandleFunc("/account", redirectSettings)

	authed.HandleFunc("GET /users", h.HandleUsers)
	authed.HandleFunc("GET /users/new", h.HandleUserNew)
	authed.HandleFunc("POST /users/new", h.HandleUserNew)
	authed.HandleFunc("GET /users/{uid}", h.HandleUserEdit)
	authed.HandleFunc("POST /users/{uid}", h.HandleUserEdit)
	authed.HandleFunc("GET /users/{uid}/delete", h.HandleUserDeleteConfirm)
	authed.HandleFunc("POST /users/{uid}/delete", h.HandleUserDelete)

	authed.HandleFunc("GET /backup", h.HandleBackupPage)
	authed.HandleFunc("POST /backup", h.HandleBackup)

	authed.HandleFunc("GET /deliveries", h.HandleDeliveries)
	authed.HandleFunc("GET /deliveries/rows", h.HandleDeliveriesRows)
	authed.HandleFunc("GET /deliveries/{id}", h.HandleDelivery)
	authed.HandleFunc("GET /mail-queue", h.HandleMailQueue)
	authed.HandleFunc("GET /mail-queue/body", h.HandleMailQueueBody)
	authed.HandleFunc("GET /system-log", h.HandleSystemLog)
	authed.HandleFunc("GET /system-log/body", h.HandleSystemLogBody)

	mux.Handle("/", s.auth.RequireAuth(authed))
	return s.secure(mux)
}

// redirectSettings sends legacy /account bookmarks to /settings (308 preserves POST).
func redirectSettings(w http.ResponseWriter, r *http.Request) {
	target := "/settings"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusPermanentRedirect)
}

func redirectHome(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromRequest(r)
	if ok && !p.IsGlobal() {
		http.Redirect(w, r, "/domains", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/status", http.StatusSeeOther)
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
