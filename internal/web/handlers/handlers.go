// Package handlers implements the panel's authenticated HTTP handlers.
package handlers

import (
	"log"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Config holds handler-specific panel configuration.
type Config struct {
	Hostname          string
	SubmissionEnabled bool
	MailLogPath       string
	DataDir           string
	DBPath            string
	DeployRoot        string
	Version           string
	TLSCertFile       string
	OpenDKIMSocket    string
	JournalSocket     string
	// Level-1 Postfix anvil backstop (env RATE_LIMIT_*), shown in the panel
	// and used to cap domain/app level-2 ceilings (guide § Rate limiting).
	RateLimitMessagesPerIP int
	RateLimitWindowSeconds int
	// RetryPolicy is this Postfix's deferred-mail timings, snapshotted once
	// when the HTTP role starts (architecture.md). The Mail queue card and
	// delivery history read it from here; they never call postconf.
	RetryPolicy postfix.RetryPolicy
	// InboundEnabled mirrors INBOUND_RELAY_ENABLE: the inbound panel and
	// routes exist only when this is true.
	InboundEnabled bool
	// SendLogRetentionEnvDefault is SEND_LOG_RETENTION_DAYS at panel start; used
	// as bootstrap and fallback when the settings row is missing or invalid.
	SendLogRetentionEnvDefault int
}

// Handlers holds dependencies for authenticated panel routes.
type Handlers struct {
	store   *store.Store
	domains *domain.Service
	apps    *app.Service
	inbound *inbound.Service
	cfg     Config
	view    *view.Engine
	dns     *dnscheck.Checker
	machine *health.MachineSampler
	auth    *auth.Module
}

// New builds authenticated panel handlers.
func New(
	st *store.Store,
	domains *domain.Service,
	apps *app.Service,
	inboundSvc *inbound.Service,
	cfg Config,
	v *view.Engine,
	dns *dnscheck.Checker,
	machine *health.MachineSampler,
	a *auth.Module,
) *Handlers {
	return &Handlers{
		store:   st,
		domains: domains,
		apps:    apps,
		inbound: inboundSvc,
		cfg:     cfg,
		view:    v,
		dns:     dns,
		machine: machine,
		auth:    a,
	}
}

func logf(format string, args ...any) {
	log.Printf(format, args...)
}
