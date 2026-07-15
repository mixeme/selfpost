// Package web implements the SelfPost control panel's HTTP surface: the
// one-time administrator setup flow (spec 7.6.1), login/session handling
// (spec 7.6.5-6) and the authenticated shell the later phases build on.
package web

import (
	"embed"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"codeberg.org/mix/selfpost/internal/app"
	"codeberg.org/mix/selfpost/internal/domain"
	"codeberg.org/mix/selfpost/internal/store"
)

//go:embed templates/*.html static/*
var assetsFS embed.FS

// Config holds the panel's HTTP-facing configuration.
type Config struct {
	// Hostname is the server's external hostname, used to build the absolute
	// setup link shown in the logs (spec 7.6.1, 8: SELFPOST_HOSTNAME).
	Hostname string
	// CookieSecure sets the Secure attribute on the session cookie. It defaults
	// to true (spec 7.6.6); it exists as a knob only so the panel can be tested
	// over plain HTTP in development, never for production.
	CookieSecure bool
	// MailLogPath is where Postfix's delivery log lives, read by the mail.log
	// monitoring view (spec 7.2.13). It is the same path the log-tailer role
	// follows in cmd/panel.
	MailLogPath string
	// DataDir and DBPath locate the persistent state a full backup archives
	// (spec 7.5.A); Version is stamped into the backup manifest. They mirror the
	// panel's own configuration.
	DataDir string
	DBPath  string
	Version string
	// TrustedProxyCIDRs are the reverse-proxy addresses allowed to supply
	// X-Forwarded-For (plan.md item A.1: TRUSTED_PROXY_CIDR). A request whose
	// direct peer (RemoteAddr) is not in this list never has its XFF header
	// honoured, so the header can't be spoofed by anyone but a trusted proxy.
	// Empty (the default) keeps rate-limiting keyed on RemoteAddr only.
	TrustedProxyCIDRs []*net.IPNet
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

	loginLimiter *rateLimiter
	setupLimiter *rateLimiter

	trustedProxies []*net.IPNet
}

// New builds the panel server. setupTokenPath is where the current setup token
// is mirrored on disk (spec 7.6.1); domains is the sending-domain service that
// owns DKIM keys and the OpenDKIM tables (spec 6); apps owns application SASL
// accounts and the Postfix sender map (spec 5.1).
func New(st *store.Store, domains *domain.Service, apps *app.Service, cfg Config, setupTokenPath string) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:    st,
		domains:  domains,
		apps:     apps,
		cfg:      cfg,
		tmpl:     tmpl,
		sessions: newSessionStore(),
		// Setup: a handful of attempts per minute per IP is plenty for a
		// legitimate admin and blunts automated probing (spec 7.6.1).
		setupLimiter: newRateLimiter(10, time.Minute),
		// Login: throttle brute-force by IP (spec 7.6.5).
		loginLimiter: newRateLimiter(10, 15*time.Minute),

		trustedProxies: cfg.TrustedProxyCIDRs,
	}
	s.setup = newSetupManager(st, cfg.Hostname, setupTokenPath)
	return s, nil
}

// Start performs first-run bootstrapping: if there is no administrator yet, it
// generates and announces the setup link (spec 7.6.1). Safe to call once at
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

	// One-time administrator setup (spec 7.6.1).
	mux.HandleFunc("/setup/", s.handleSetup)

	// Authentication.
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// Authenticated panel. Everything not matched by a more specific pattern
	// above falls through to this sub-mux, wrapped once in the auth middleware.
	authed := http.NewServeMux()
	authed.HandleFunc("GET /{$}", s.handleDashboard)
	authed.HandleFunc("POST /domains", s.handleAddDomain)
	authed.HandleFunc("POST /domains/import", s.handleImportDomain)
	authed.HandleFunc("GET /domains/{id}", s.handleDomainDetail)
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

	// Full-server backup download (spec 7.5.A).
	authed.HandleFunc("POST /backup", s.handleBackup)

	// Monitoring screens (spec 7.2.11-13): each page and its HTMX polling
	// fragment (spec 7.1 — the /rows and /body endpoints return HTML, not JSON).
	authed.HandleFunc("GET /sendlog", s.handleSendLog)
	authed.HandleFunc("GET /sendlog/rows", s.handleSendLogRows)
	authed.HandleFunc("GET /queue", s.handleQueue)
	authed.HandleFunc("GET /queue/body", s.handleQueueBody)
	authed.HandleFunc("GET /logtail", s.handleLogTail)
	authed.HandleFunc("GET /logtail/body", s.handleLogTailBody)

	mux.Handle("/", s.requireAuth(authed))

	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// clientIP extracts the peer IP for rate-limiting. By default it is the
// transport peer (RemoteAddr), which cannot be spoofed. If RemoteAddr matches
// one of trustedProxies, the last entry of X-Forwarded-For is used instead —
// that is the address the trusted proxy itself appended, so a client can't
// forge it by sending its own XFF header (plan.md item A.1). With no trusted
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
