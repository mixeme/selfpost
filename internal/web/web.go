// Package web implements the SelfPost control panel's HTTP surface: the
// one-time administrator setup flow (security.md), login/session handling
// (security.md) and the authenticated shell the later phases build on.
package web

import (
	"embed"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
)

//go:embed templates/*.html static/*
var assetsFS embed.FS

// Config holds the panel's HTTP-facing configuration.
type Config struct {
	// Hostname is the server's external hostname, used to build the absolute
	// setup link shown in the logs (security.md; README § Environment
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
	// (architecture.md § Persistence); Version is stamped into the backup
	// manifest. They mirror the panel's own configuration.
	DataDir string
	DBPath  string
	Version string
	// TrustedProxyCIDRs are the reverse-proxy addresses allowed to supply
	// X-Forwarded-For (env TRUSTED_PROXY_CIDR). A request whose
	// direct peer (RemoteAddr) is not in this list never has its XFF header
	// honoured, so the header can't be spoofed by anyone but a trusted proxy.
	// Empty (the default) keeps rate-limiting keyed on RemoteAddr only.
	TrustedProxyCIDRs []*net.IPNet
	// TLSCertFile is the certificate Postfix serves on 465/587 (README §
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
}

// Server is the panel HTTP application.
type Server struct {
	store    *store.Store
	domains  *domain.Service
	apps     *app.Service
	cfg      Config
	tmpl     *templates
	sessions *sessionStore
	setup    *setupManager
	dns      *dnscheck.Checker
	// machine reads the host's CPU, memory and network counters for the
	// status page. It has to be one shared sampler for the whole server:
	// CPU and throughput are differences between successive readings, so a
	// per-request sampler would never have a previous one to subtract.
	machine health.MachineSampler

	loginLimiter *rateLimiter
	setupLimiter *rateLimiter

	trustedProxies []*net.IPNet
}

// New builds the panel server. setupTokenPath is where the current setup token
// is mirrored on disk (security.md); domains is the sending-domain service
// that owns DKIM keys and the OpenDKIM tables (architecture.md § OpenDKIM);
// apps owns application SASL accounts and the Postfix sender map
// (architecture.md § Mail path).
func New(st *store.Store, domains *domain.Service, apps *app.Service, cfg Config, setupTokenPath string) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	idleDays := cfg.SessionIdleDays
	if idleDays <= 0 {
		idleDays = 7
	}
	s := &Server{
		store:    st,
		domains:  domains,
		apps:     apps,
		cfg:      cfg,
		tmpl:     tmpl,
		sessions: newSessionStore(st, time.Duration(idleDays)*24*time.Hour),
		// Published-DNS checks for the status page and the domain pages. The
		// checker caches its own results, so page views do not each pay for a
		// round of lookups.
		dns: dnscheck.New(cfg.DNSResolvers),
		// Setup: a handful of attempts per minute per IP is plenty for a
		// legitimate admin and blunts automated probing (security.md).
		setupLimiter: newRateLimiter(10, time.Minute),
		// Login: throttle brute-force by IP (security.md).
		loginLimiter: newRateLimiter(10, 15*time.Minute),

		trustedProxies: cfg.TrustedProxyCIDRs,
	}
	s.setup = newSetupManager(st, cfg.Hostname, setupTokenPath)
	return s, nil
}

// Start performs first-run bootstrapping: if there is no administrator yet, it
// generates and announces the setup link (security.md). Safe to call once at
// server startup.
func (s *Server) Start() error {
	return s.setup.bootstrap()
}

// Handler returns the panel's HTTP handler (router).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check stays unauthenticated for the container/orchestrator.
	mux.HandleFunc("/healthz", handleHealth)

	// Vendored static assets (HTMX). Served from the embedded FS.
	mux.Handle("/static/", http.FileServer(http.FS(assetsFS)))

	// One-time administrator setup (security.md).
	mux.HandleFunc("/setup/", s.handleSetup)

	// Authentication.
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// Authenticated panel. Everything not matched by a more specific pattern
	// above falls through to this sub-mux, wrapped once in the auth middleware.
	authed := http.NewServeMux()

	// The landing page is the server status: the first thing an
	// administrator should see after logging in is whether the service is
	// healthy, not the domain list. handleLogin still redirects to "/".
	authed.HandleFunc("GET /{$}", redirectToStatus)
	authed.HandleFunc("GET /status", s.handleStatus)
	authed.HandleFunc("GET /status/fragment", s.handleStatusFragment)
	authed.HandleFunc("POST /status/recheck", s.handleStatusRecheck)

	authed.HandleFunc("GET /domains", s.handleDashboard)
	authed.HandleFunc("POST /domains", s.handleAddDomain)
	authed.HandleFunc("POST /domains/import", s.handleImportDomain)
	authed.HandleFunc("GET /domains/{id}", s.handleDomainDetail)
	authed.HandleFunc("POST /domains/{id}/dns-recheck", s.handleDomainDNSRecheck)
	authed.HandleFunc("GET /domains/{id}/delete", s.handleDeleteConfirm)
	authed.HandleFunc("POST /domains/{id}/delete", s.handleDeleteDomain)
	authed.HandleFunc("POST /domains/{id}/applications", s.handleAddApplication)
	authed.HandleFunc("POST /domains/{id}/ratelimit", s.handleDomainRateLimit)
	authed.HandleFunc("POST /domains/{id}/export", s.handleExportDomain)
	authed.HandleFunc("POST /applications/{aid}/mode", s.handleUpdateAppMode)
	authed.HandleFunc("POST /applications/{aid}/password", s.handleRegenPassword)
	authed.HandleFunc("POST /applications/{aid}/ratelimit", s.handleAppRateLimit)
	authed.HandleFunc("POST /applications/{aid}/delete", s.handleDeleteApplication)
	authed.HandleFunc("POST /reload", s.handleReload)

	// Administrator's own panel credentials.
	authed.HandleFunc("/account", s.handleAccount)

	// Backup and migration: the page with both actions (architecture.md §
	// Persistence-B), and the full-server backup download itself.
	authed.HandleFunc("GET /backup", s.handleBackupPage)
	authed.HandleFunc("POST /backup", s.handleBackup)

	// Monitoring screens (architecture.md § Panel HTTP surface): each page and
	// its HTMX polling fragment (architecture.md § Panel HTTP surface — the /rows
	// and /body endpoints return HTML, not JSON).
	authed.HandleFunc("GET /deliveries", s.handleDeliveries)
	authed.HandleFunc("GET /deliveries/rows", s.handleDeliveriesRows)
	authed.HandleFunc("GET /deliveries/{id}", s.handleDelivery)
	authed.HandleFunc("GET /mail-queue", s.handleMailQueue)
	authed.HandleFunc("GET /mail-queue/body", s.handleMailQueueBody)
	authed.HandleFunc("GET /system-log", s.handleSystemLog)
	authed.HandleFunc("GET /system-log/body", s.handleSystemLogBody)

	mux.Handle("/", s.requireAuth(authed))

	// Security headers and the origin check wrap everything, including the
	// unauthenticated login and setup routes.
	return s.secure(mux)
}

// redirectToStatus points the panel root at the status page, so there is one
// canonical URL for that content instead of two.
func redirectToStatus(w http.ResponseWriter, r *http.Request) {
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

// clientIP extracts the peer IP for rate-limiting. By default it is the
// transport peer (RemoteAddr), which cannot be spoofed. If RemoteAddr matches
// one of trustedProxies, the last entry of X-Forwarded-For is used instead —
// that is the address the trusted proxy itself appended, so a client can't
// forge it by sending its own XFF header. With no trusted
// proxies configured, behind a reverse proxy this is the proxy's own address,
// which is an acceptable backstop for a single-admin panel.
func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if len(trustedProxies) > 0 {
		if peer := net.ParseIP(host); peer != nil && ipInAny(peer, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
					return ip.String()
				}
			}
		}
	}

	return host
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// logf is a thin wrapper so handlers log with a consistent prefix.
func logf(format string, args ...any) {
	log.Printf(format, args...)
}
