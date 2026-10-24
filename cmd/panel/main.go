// Command panel is the SelfPost control panel. This single binary combines
// several roles (architecture.md § Image and processes) as a supervised
// process: the HTTP panel server, the journal-milter, the mail.log tailer and
// the rate-limit checks.
//
// Copyright (C) 2026 Mikhail Yenuchenko
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/logtail"
	"github.com/mixeme/selfpost/internal/store"
)

func main() {
	if isDMARCIngestInvocation() {
		log.SetFlags(log.LstdFlags | log.LUTC)
		log.SetPrefix("dmarc-ingest: ")
		if err := runDMARCIngestMode(); err != nil {
			log.Printf("ingest failed: %v", err)
			// EX_TEMPFAIL — ask Postfix to defer. Postfix 3.10.13,
			// src/global/pipe_command.c, pipe_command(): a sysexits code maps
			// to its DSN (src/global/sys_exits.c: 75 is "4.3.0") and a 4 defers,
			// unless the output starts with a "D.S.N text" of its own - the
			// log prefix above makes sure it does not.
			os.Exit(75)
		}
		return
	}

	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix("panel: ")

	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

// config holds the runtime knobs the panel reads from the environment. Every
// value has a safe default so the binary also runs outside the container.
type config struct {
	httpAddr      string
	journalSocket string
	// The sender check's socket; the default mirrors build/postfix-config.sh.
	senderSocket string
	// The DKIM signer's sockets: applications' mail and the panel's own
	// (cmd/panel/signer.go).
	signSocket, signLocalSocket string
	mailLog                     string
	retentionDays               int

	dataDir                string
	dbPath                 string
	manifestPath           string
	setupTokenPath         string
	hostname               string
	cookieSecure           bool
	submissionEnabled      bool
	trustedProxies         []*net.IPNet
	sessionIdleDays        int
	dnsResolvers           []string
	rateLimitMessagesPerIP int
	rateLimitWindowSeconds int

	// Read-only input to the panel's status page: the certificate Postfix
	// serves. The default mirrors build/postfix-config.sh, so the status
	// page checks exactly what Postfix was configured with.
	tlsCertFile string
	// tlsServedCert follows the certificate Postfix serves (its copy of
	// tlsCertFile) in postconf; tlsReloadInterval is how often
	// build/postfix-cert-reload.sh looks for a renewal.
	tlsServedCert     *health.ServedCert
	tlsReloadInterval time.Duration
	// smtpBanner is Postfix's smtpd_banner, expanded, read from postconf at
	// the start: Health shows the greeting it makes.
	smtpBanner string
	// timeZone is TZ as the container was started with ("" for none): the
	// zone of every time and day the panel shows, which Settings names.
	timeZone string

	dkimDir         string
	dkimSelectorDef string

	saslDBPath string
	saslRealm  string
	postfixDir string
	deployRoot string
	// Where the filter lists are written as map files for the external spam
	// filter; the antispam compose fragment mounts it into the sidecar.
	antispamDir string

	inboundEnabled bool
	// The optional milter on the inbound listener (INBOUND_ANTISPAM_MILTER) and
	// what Postfix does when it is down. Read-only: build/postfix-config.sh is
	// what attaches it; the panel only says so. See antispamAction.
	inboundAntispamMilter string
	inboundAntispamAction string
	// The inbound journal milter's socket; the default mirrors
	// build/postfix-config.sh. Served only while the inbound relay is on.
	inboundJournalSocket string
	// Retention of the inbound journal until Server > Settings says otherwise.
	inboundSpamLogRetentionDays int
	dmarcEnabled                bool
}

func loadConfig() config {
	dataDir := envDefault("SELFPOST_DATA_DIR", "/data")
	return config{
		httpAddr:      envDefault("PANEL_HTTP_ADDR", ":8080"),
		journalSocket: envDefault("JOURNAL_MILTER_SOCKET", "/run/selfpost/journal.sock"),
		senderSocket:  envDefault("SENDER_CHECK_SOCKET", "/run/selfpost/sender.sock"),
		// The DKIM signer's two sockets; the defaults must match
		// build/postfix-config.sh and build/postfix-wrapper.sh.
		signSocket:      envDefault("DKIM_SIGN_SOCKET", "/run/selfpost/sign.sock"),
		signLocalSocket: envDefault("DKIM_SIGN_LOCAL_SOCKET", "/run/selfpost/sign-local.sock"),
		// Postfix's delivery log, under /data so the lines that resolve a
		// "queued" send-log row outlive the container. The default must match
		// maillog_file in build/postfix-config.sh.
		mailLog: envDefault("MAIL_LOG", "/data/log/mail.log"),
		// Send-log retention window (architecture.md § Persistence).
		// Non-positive/invalid falls back to the 90-day default inside the
		// log-tailer.
		retentionDays: envInt("SEND_LOG_RETENTION_DAYS", 90),

		dataDir:        dataDir,
		dbPath:         envDefault("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db")),
		manifestPath:   filepath.Join(dataDir, backup.ManifestName),
		setupTokenPath: envDefault("SELFPOST_SETUP_TOKEN_FILE", filepath.Join(dataDir, "setup-token")),
		hostname:       os.Getenv("SELFPOST_HOSTNAME"),
		timeZone:       os.Getenv("TZ"),
		// Secure cookies by default (security.md); PANEL_COOKIE_SECURE=false is a
		// development-only escape hatch for testing over plain HTTP.
		cookieSecure: envDefault("PANEL_COOKIE_SECURE", "true") != "false",
		// Whether this deployment also runs the 587 submission listener. The
		// panel only displays it as a client connection setting; the comparison
		// matches postfix-config.sh, which enables the listener on "true" alone.
		submissionEnabled: os.Getenv("SUBMISSION_ENABLE") == "true",
		// Reverse-proxy addresses allowed to supply X-Forwarded-For for
		// rate-limiting. Empty by default: an untrusted peer's
		// XFF header is trivially forgeable, so it's ignored unless the panel is
		// told which proxy to trust.
		trustedProxies: parseTrustedProxies(os.Getenv("TRUSTED_PROXY_CIDR")),
		// Sliding session idle timeout (security.md, plan B.1). Non-positive/invalid
		// falls back to the 7-day default inside internal/web.
		sessionIdleDays: envInt("PANEL_SESSION_IDLE_DAYS", 7),
		// Recursive resolvers the deliverability checks query directly. Empty
		// means dnscheck's public defaults; a closed network names its own here.
		dnsResolvers: dnscheck.ParseResolvers(os.Getenv("SELFPOST_DNS_RESOLVERS")),

		// Level-1 anvil defaults match build/postfix-config.sh / guide.md.
		rateLimitMessagesPerIP: envInt("RATE_LIMIT_MESSAGES_PER_IP", 100),
		rateLimitWindowSeconds: envInt("RATE_LIMIT_WINDOW_SECONDS", 3600),

		tlsCertFile:       envDefault("TLS_CERT_FILE", "/etc/postfix/tls/fullchain.pem"),
		tlsReloadInterval: time.Duration(envInt("TLS_RELOAD_INTERVAL_SECONDS", 86400)) * time.Second,

		// Per-domain DKIM state (architecture.md § DKIM signing): the keys and
		// the selector table, the layout entrypoint.sh prepares.
		dkimDir:         envDefault("DKIM_DIR", filepath.Join(dataDir, "dkim")),
		dkimSelectorDef: envDefault("DKIM_SELECTOR_DEFAULT", "selfpost"),

		// Application SASL accounts and the Postfix sender map (architecture.md §
		// Mail path), both under /data so they survive restarts. The SASL realm
		// defaults to the server hostname so account identities line up with
		// Postfix's SASL configuration; it falls back to localhost outside the
		// container.
		saslDBPath: envDefault("SASL_DB_PATH", filepath.Join(dataDir, "sasl", "sasldb2")),
		saslRealm:  saslRealm(),
		postfixDir: envDefault("POSTFIX_DIR", filepath.Join(dataDir, "postfix")),

		antispamDir: envDefault("ANTISPAM_DIR", filepath.Join(dataDir, "antispam")),
		deployRoot:  envDefault("SELFPOST_DEPLOY_ROOT", "/selfpost-deploy"),
		// Optional inbound relay (backup-MX / forwarder). Off unless exactly "true".
		inboundEnabled:        os.Getenv("INBOUND_RELAY_ENABLE") == "true",
		inboundAntispamMilter: os.Getenv("INBOUND_ANTISPAM_MILTER"),
		inboundAntispamAction: antispamAction(os.Getenv("INBOUND_ANTISPAM_MILTER_ACTION")),
		inboundJournalSocket:  envDefault("INBOUND_JOURNAL_MILTER_SOCKET", "/run/selfpost/inbound-journal.sock"),

		inboundSpamLogRetentionDays: envInt("INBOUND_SPAM_LOG_RETENTION_DAYS", store.InboundSpamLogRetentionDaysDefault),
		// Optional DMARC aggregate ingest on port 25. Off unless exactly "true".
		dmarcEnabled: os.Getenv("DMARC_REPORTS_ENABLE") == "true",
	}
}

// antispamAction reads INBOUND_ANTISPAM_MILTER_ACTION the way
// build/postfix-config.sh does: empty means accept, and accept and tempfail are
// the only other values it lets the container start with. Anything else is
// returned as "" — the panel says nothing about the action rather than guess.
func antispamAction(raw string) string {
	switch raw {
	case "", "accept":
		return "accept"
	case "tempfail":
		return "tempfail"
	}
	return ""
}

// saslRealm chooses the realm new SASL accounts live under. It mirrors the
// hostname Postfix's SASL layer uses so a client authenticating with a bare
// login resolves to the right account. Postfix 3.10.13,
// src/xsasl/xsasl_cyrus_server.c, xsasl_cyrus_server_create(): serverFQDN is
// $myhostname (= SELFPOST_HOSTNAME, build/postfix-config.sh) and user_realm
// NULL while smtpd_sasl_local_domain is empty; Cyrus SASL 2.1.28,
// plugins/sasldb.c, sasldb_auxprop_lookup(), and common/plugin_common.c,
// _plug_parseuser(): a login
// without "@" is then looked up in the realm serverFQDN. A SASL_REALM other
// than SELFPOST_HOSTNAME does not follow that - assumptions A-48.
func saslRealm() string {
	if r := os.Getenv("SASL_REALM"); r != "" {
		return r
	}
	if h := os.Getenv("SELFPOST_HOSTNAME"); h != "" {
		return h
	}
	return "localhost"
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt reads an integer environment variable, returning def if it is unset or
// not a valid integer.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("ignoring invalid %s=%q, using %d", key, v, def)
	}
	return def
}

// parseTrustedProxies parses a comma-separated list of CIDRs (bare IPs are
// accepted and treated as /32 or /128). Invalid entries are logged and
// skipped rather than failing startup, matching envInt's tolerance of
// misconfiguration.
func parseTrustedProxies(raw string) []*net.IPNet {
	if raw == "" {
		return nil
	}
	var nets []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		if !strings.Contains(cidr, "/") {
			// The prefix length goes with how the address is written: an
			// IPv4-mapped address written as IPv6 (::ffff:10.0.0.1) is a
			// 128-bit address, and "/32" on it is ::/32 - every address
			// whose first 32 bits are zero (::1 among them) trusted, and the
			// host meant not at all (audit 2026-10-12, L4). Go 1.26
			// net.IPNet.Contains compares an IPv4 client with the mapped
			// /128 as with 10.0.0.1/32 (net/ip.go, networkNumberAndMask).
			if strings.Contains(cidr, ":") {
				cidr += "/128"
			} else {
				cidr += "/32"
			}
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("ignoring invalid TRUSTED_PROXY_CIDR entry %q: %v", part, err)
			continue
		}
		nets = append(nets, n)
	}
	return nets
}

// openData opens the database the panel runs on. When a backup was unpacked
// into /data, the restore guard comes first (architecture.md § Persistence):
// the manifest's version must match this binary and the archive must have
// been unpacked whole before the database is touched, so neither schema skew
// nor half an archive can corrupt the restored state. That first start then
// runs resync, and only after it the manifest is consumed: a start that fails
// on the way leaves it, and the next start is a restore again (audit
// 2026-10-12, M1). Without a manifest it is an ordinary start.
func openData(cfg config, resync func(*store.Store) error) (st *store.Store, restored bool, err error) {
	restored, err = backup.CheckRestore(cfg.manifestPath, buildinfo.Version)
	if err != nil {
		return nil, false, err
	}
	st, err = store.Open(cfg.dbPath)
	if err != nil {
		return nil, false, err
	}
	if restored {
		log.Printf("restore manifest accepted; regenerating mail-path maps from SQLite")
		// A link taken after the backup was made would work again from it
		// (security review, 2026-10-17); a link sent before a restore is
		// asked for again.
		if err := st.DeleteAllUserLinks(); err != nil {
			_ = st.Close()
			return nil, false, err
		}
		if err := resync(st); err != nil {
			_ = st.Close()
			return nil, false, err
		}
		if err := backup.ConsumeRestore(cfg.manifestPath); err != nil {
			_ = st.Close()
			return nil, false, err
		}
	}
	return st, restored, nil
}

// run starts the panel's roles (the list below) and blocks until a shutdown
// signal or the first fatal error from any role. A signal triggers a clean stop of all
// roles; a role error cancels the others and is returned so the process exits
// non-zero (letting supervisord/Docker see the failure — architecture.md §
// Image and processes).
func run() error {
	cfg := loadConfig()
	// postfix-config.sh has written main.cf before supervisord started the
	// panel (build/entrypoint.sh); after, only cert-reload changes it, and
	// only the certificate's path (health.ServedCert).
	cfg.tlsServedCert = health.NewServedCert(cfg.tlsCertFile)
	cfg.smtpBanner = health.SMTPBanner()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("starting selfpost panel %s", buildinfo.Version)

	// One database handle shared by every role. The store serialises writes
	// (MaxOpenConns(1)), so the HTTP panel, the journal-milter and the tailer
	// can all use it without stepping on each other under WAL.
	st, restored, err := openData(cfg, func(st *store.Store) error {
		return resyncAfterRestore(cfg, st, false)
	})
	if err != nil {
		return err
	}
	defer st.Close()

	if !restored && cfg.dmarcEnabled {
		ms := newMailStack(cfg, st)
		ms.pf.SetReloadHook(func() error { return nil })
		if err := ms.DMARC.Resync(); err != nil {
			log.Printf("dmarc maps bootstrap: %v", err)
		}
	}

	// The filter lists as files, on every start: the spam filter's
	// configuration names all six, and a data directory from before the lists
	// existed has none. Not fatal - the lists are the filter's business, and
	// the panel has mail to relay either way.
	if cfg.inboundEnabled && !restored {
		if err := inbound.NewLists(st, cfg.antispamDir).Sync(); err != nil {
			log.Printf("filter lists bootstrap: %v", err)
		}
	}
	// The key the filter signs with has to be in the shared directory before
	// the filter reads it, whether or not a filter is attached yet.
	if cfg.inboundEnabled {
		if _, err := inbound.FilterKey(cfg.antispamDir); err != nil {
			log.Printf("filter key bootstrap: %v", err)
		}
	}

	if err := st.EnsureSendLogRetentionDays(cfg.retentionDays); err != nil {
		return fmt.Errorf("bootstrap send-log retention: %w", err)
	}
	// With the inbound relay on, the tailer also writes the inbound journal's
	// rows for refused mail and prunes that journal.
	var inboundTail *logtail.Inbound
	if cfg.inboundEnabled {
		if err := st.EnsureInboundSpamLogRetentionDays(cfg.inboundSpamLogRetentionDays); err != nil {
			return fmt.Errorf("bootstrap inbound journal retention: %w", err)
		}
		inboundTail = &logtail.Inbound{Store: st, Retention: func() int {
			days, err := st.GetInboundSpamLogRetentionDays(cfg.inboundSpamLogRetentionDays)
			if err != nil {
				log.Printf("inbound journal retention: %v", err)
				return cfg.inboundSpamLogRetentionDays
			}
			return days
		}}
	}

	var wg sync.WaitGroup
	// Room for every role to report a failure without blocking: eight at most.
	errc := make(chan error, 8)

	roles := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"http", func(ctx context.Context) error { return serveHTTP(ctx, cfg, st) }},
		{"journal-milter", func(ctx context.Context) error { return serveJournal(ctx, cfg, st) }},
		{"sender-check", func(ctx context.Context) error { return serveSenderCheck(ctx, cfg, st) }},
		{"dkim-signer", func(ctx context.Context) error { return serveSigner(ctx, cfg, st) }},
		{"log-tailer", func(ctx context.Context) error {
			return logtail.Run(ctx, cfg.mailLog, st, func() int {
				days, err := st.GetSendLogRetentionDays(cfg.retentionDays)
				if err != nil {
					log.Printf("send-log retention: %v", err)
					return cfg.retentionDays
				}
				return days
			}, inboundTail, &logtail.SenderCheck{Store: st, Socket: cfg.senderSocket})
		}},
		{"rate-limit-recalc", func(ctx context.Context) error {
			runAutoRateLimitRecalc(ctx, cfg, st)
			return nil
		}},
		{"notifier", func(ctx context.Context) error { return runNotifier(ctx, cfg, st) }},
	}

	if cfg.inboundEnabled {
		roles = append(roles, struct {
			name string
			fn   func(context.Context) error
		}{"inbound-journal-milter", func(ctx context.Context) error { return serveInboundJournal(ctx, cfg, st) }})
	}

	for _, r := range roles {
		wg.Add(1)
		go func(name string, fn func(context.Context) error) {
			defer wg.Done()
			if err := fn(ctx); err != nil {
				errc <- fmt.Errorf("%s: %w", name, err)
			}
		}(r.name, r.fn)
	}

	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received, stopping")
		wg.Wait()
		log.Printf("panel stopped cleanly")
		return nil
	case err := <-errc:
		log.Printf("role failed: %v", err)
		stop() // cancel ctx so the other roles wind down
		wg.Wait()
		return err
	}
}
