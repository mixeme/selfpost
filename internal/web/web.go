// Package web implements the SelfPost control panel's HTTP surface: the
// one-time administrator setup flow (spec 7.6.1), login/session handling
// (spec 7.6.5-6) and the authenticated shell the later phases build on.
package web

import (
	"embed"
	"log"
	"net"
	"net/http"
	"time"

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
}

// Server is the panel HTTP application.
type Server struct {
	store    *store.Store
	cfg      Config
	tmpl     *templates
	sessions *sessionStore
	setup    *setupManager

	loginLimiter *rateLimiter
	setupLimiter *rateLimiter
}

// New builds the panel server. setupTokenPath is where the current setup token
// is mirrored on disk (spec 7.6.1).
func New(st *store.Store, cfg Config, setupTokenPath string) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:    st,
		cfg:      cfg,
		tmpl:     tmpl,
		sessions: newSessionStore(),
		// Setup: a handful of attempts per minute per IP is plenty for a
		// legitimate admin and blunts automated probing (spec 7.6.1).
		setupLimiter: newRateLimiter(10, time.Minute),
		// Login: throttle brute-force by IP (spec 7.6.5).
		loginLimiter: newRateLimiter(10, 15*time.Minute),
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

	// Authenticated panel.
	mux.Handle("/", s.requireAuth(http.HandlerFunc(s.handleDashboard)))

	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// clientIP extracts the peer IP for rate-limiting. It uses the transport peer
// (RemoteAddr), not client-supplied headers, so it cannot be spoofed; behind a
// reverse proxy this is the proxy address, which is an acceptable backstop for
// a single-admin panel.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// logf is a thin wrapper so handlers log with a consistent prefix.
func logf(format string, args ...any) {
	log.Printf(format, args...)
}
