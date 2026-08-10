// Package handlers implements the panel's authenticated HTTP handlers.
package handlers

import (
	"log"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
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
	Version           string
	TLSCertFile       string
	OpenDKIMSocket    string
	JournalSocket     string
}

// Handlers holds dependencies for authenticated panel routes.
type Handlers struct {
	store   *store.Store
	domains *domain.Service
	apps    *app.Service
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
