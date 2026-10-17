package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/secretfile"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
	"golang.org/x/crypto/bcrypt"
)

// maxImportBytes caps a domain-import upload. A domain export is a small JSON
// document (a DKIM key and a handful of credentials); this leaves generous head
// room while refusing anything large enough to be an abuse attempt. An
// encrypted export adds only a header and per-chunk tags, so the same ceiling
// covers both forms.
const maxImportBytes = 1 << 20 // 1 MiB

// HandleBackupPage renders the backup/migration screen: the full-server backup
// and the domain import are separate actions with different risk, so each gets
// its own card here rather than sharing a block on the domain list.
func (h *Handlers) HandleBackupPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.renderBackupPage(w, r, http.StatusOK, "")
}

// renderBackupPage draws the page; importErr surfaces a failed domain import
// (architecture.md § Persistence) next to the form that produced it.
func (h *Handlers) renderBackupPage(w http.ResponseWriter, r *http.Request, status int, importErr string) {
	h.renderBackupPageWith(w, r, status, importErr, "")
}

// renderBackupPageWith is renderBackupPage with the second of the page's two
// error slots: backupErr belongs to the full-backup card (a rejected encryption
// password), importErr to the import card, so neither message appears under the
// wrong form.
func (h *Handlers) renderBackupPageWith(w http.ResponseWriter, r *http.Request, status int, importErr, backupErr string) {
	refusal := importErr
	if refusal == "" {
		refusal = backupErr
	}
	h.view.Render(w, status, "backup", view.NewBackup(h.shellMeta(r), validate.MinSecretFilePasswordLen, refusal))
}

// HandleBackup streams a full-server backup as a download (architecture.md §
// Persistence). It is an authenticated admin action (this handler sits behind
// the auth middleware). The archive carries DKIM private keys, the admin
// password hash and SASL credentials, so it is served with no-store and as an
// attachment to discourage caching of secret material. When the operator ticks
// "encrypt with a password", the archive is wrapped in a .spbk envelope on the
// way out, so the file that lands on their disk — wherever it is copied
// afterwards — is useless without the password.
func (h *Handlers) HandleBackup(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	if status, msg := h.reauth(r, "full backup"); status != 0 {
		h.renderBackupPageWith(w, r, status, "", msg)
		return
	}
	password, pwErr := secretFilePassword(r)
	if pwErr != "" {
		h.renderBackupPageWith(w, r, http.StatusBadRequest, "", pwErr)
		return
	}
	params := backup.Params{
		DataDir:    h.cfg.DataDir,
		DBPath:     h.cfg.DBPath,
		Version:    h.cfg.Version,
		DeployRoot: h.cfg.DeployRoot,
		OnWarn: func(msg string) {
			logf("panel: full backup: %s", msg)
		},
	}
	// The server's write deadline is for pages (cmd/panel newHTTPServer); an
	// archive streams for as long as it takes, and the read-through before it
	// (Check) is as long as the archive: the deadline moves first, so a
	// refusal after a long Check can still be written (review of stage 4).
	// Two hours carry a gigabyte at a little over 1 Mbit/s, and still end a
	// client that keeps the connection and stops reading, which TCP alone
	// would not (net/http, Go 1.26: ResponseController.SetWriteDeadline).
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Hour)); err != nil {
		logf("panel: full backup: lift the write deadline: %v", err)
	}

	if err := backup.ValidateDeployRoot(h.cfg.DeployRoot); err != nil {
		logf("panel: full backup: %v", err)
		h.renderBackupPageWith(w, r, http.StatusBadRequest, "", deployBackupErr(err))
		return
	}
	// Everything is read through once before the first byte goes out. Past the
	// headers a failure can only cut the download short - which is what the
	// button did from 1.3.0 to 2.0.0, with a 200 and ten bytes, because the
	// queue was not the panel's to read. Nothing is skipped to make the archive
	// possible: it is the one selfpost-backup makes, or it is refused.
	if err := backup.Check(params); err != nil {
		logf("panel: full backup: %v", err)
		h.renderBackupPageWith(w, r, http.StatusInternalServerError, "", unreadableBackupErr(err, h.cfg.DeployRoot))
		return
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("selfpost-backup-%s.tar.gz", stamp)
	contentType := "application/gzip"
	if password != "" {
		filename = fmt.Sprintf("selfpost-backup-%s%s", stamp, secretfile.ExtBackup)
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")

	// Everything below streams: past this point headers (and possibly some
	// bytes) are already on the wire, so a failure cannot switch to a clean
	// error page. Log it and let the truncated download fail loudly on the
	// client side — for an encrypted archive that is a missing end-of-stream
	// chunk, which decryption refuses outright.
	sink := io.Writer(w)
	var env *secretfile.Writer
	if password != "" {
		var err error
		// The only failures here are key derivation (which happens before
		// anything is written) and writing the envelope header, which fails only
		// if the client is already gone.
		env, err = secretfile.NewWriter(w, secretfile.TypeFullBackup, password)
		if err != nil {
			logf("panel: full backup: encrypt: %v", err)
			http.Error(w, "backup failed", http.StatusInternalServerError)
			return
		}
		sink = env
	}

	if err := backup.Create(sink, params); err != nil {
		logf("panel: full backup failed: %v", err)
		return
	}
	if env != nil {
		if err := env.Close(); err != nil {
			logf("panel: full backup failed: %v", err)
		}
	}
}

// HandleExportDomain streams a single-domain export as a secret download
// (architecture.md § Persistence). Like the full backup it is POST-only (state
// is not changed, but the response contains the domain's DKIM private key and
// application passwords, so it must not be prefetchable or cached). Like the
// full backup it can be encrypted with a password, in which case the download
// is a .spde envelope instead of plain JSON.
func (h *Handlers) HandleExportDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	if status, msg := h.reauth(r, "export of domain "+strconv.FormatInt(d.ID, 10)); status != 0 {
		h.renderDomainSettings(w, r, status, d, settingsView{Err: msg})
		return
	}
	password, pwErr := secretFilePassword(r)
	if pwErr != "" {
		h.renderDomainSettings(w, r, http.StatusBadRequest, d, settingsView{Err: pwErr})
		return
	}
	exp, err := h.domains.Export(d.ID)
	if err != nil {
		logf("panel: export domain %d: %v", d.ID, err)
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	body, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		logf("panel: export domain %d: encode: %v", d.ID, err)
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("selfpost-domain-%s.json", d.Name)
	contentType := "application/json"
	if password != "" {
		// An export is small, so it is sealed in memory: the response is only
		// started once the ciphertext is complete and nothing can half-fail.
		var buf bytes.Buffer
		env, err := secretfile.NewWriter(&buf, secretfile.TypeDomainExport, password)
		if err == nil {
			_, err = env.Write(body)
		}
		if err == nil {
			err = env.Close()
		}
		if err != nil {
			logf("panel: export domain %d: encrypt: %v", d.ID, err)
			http.Error(w, "export failed", http.StatusInternalServerError)
			return
		}
		body = buf.Bytes()
		filename = fmt.Sprintf("selfpost-domain-%s%s", d.Name, secretfile.ExtDomainExport)
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// HandleImportDomain accepts an uploaded domain-export file and re-creates the
// domain on this instance (architecture.md § Persistence). The domain name is
// normalised and validated here (security.md); the domain service validates
// the selector, each login and address, and the DKIM key before writing
// anything. On success it redirects to the new domain's page; on failure it
// re-renders the backup page, where the import form lives, with a friendly
// message.
func (h *Handlers) HandleImportDomain(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		h.renderBackupPage(w, r, http.StatusBadRequest, "Could not read the uploaded file (too large or not a valid upload).")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		h.renderBackupPage(w, r, http.StatusBadRequest, "Choose a domain export file to import.")
		return
	}
	defer file.Close()

	// An encrypted export announces itself with the envelope magic, so the file
	// decides which path it takes; the password field is only consulted when the
	// file actually needs it, and a password typed for a plain file is a plain
	// mistake worth reporting.
	head := make([]byte, secretfile.MagicLen)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		h.renderBackupPage(w, r, http.StatusBadRequest, "Could not read the uploaded file.")
		return
	}
	source := io.MultiReader(bytes.NewReader(head[:n]), file)
	password := r.PostFormValue("import_password")

	if secretfile.HasMagic(head[:n]) {
		if password == "" {
			h.renderBackupPage(w, r, http.StatusBadRequest, "That file is encrypted — enter the password it was exported with.")
			return
		}
		env, err := secretfile.NewReader(source, password)
		if err != nil {
			h.renderBackupPage(w, r, http.StatusBadRequest, decryptErrorMessage(err))
			return
		}
		if env.Type() != secretfile.TypeDomainExport {
			h.renderBackupPage(w, r, http.StatusBadRequest, "That file is an encrypted "+env.Type().String()+", not a domain export.")
			return
		}
		// Read the whole plaintext first: authentication of the last chunk is
		// what proves the file is intact, and a streaming JSON decoder could
		// accept a truncated document before ever reaching it.
		plain, err := io.ReadAll(env)
		if err != nil {
			h.renderBackupPage(w, r, http.StatusBadRequest, decryptErrorMessage(err))
			return
		}
		source = bytes.NewReader(plain)
	} else if password != "" {
		h.renderBackupPage(w, r, http.StatusBadRequest, "That file is not encrypted — leave the password empty.")
		return
	}

	var exp domain.DomainExport
	dec := json.NewDecoder(source)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&exp); err != nil {
		h.renderBackupPage(w, r, http.StatusBadRequest, "That file is not a valid SelfPost domain export.")
		return
	}

	// Normalise and validate the domain name before it reaches the service, the
	// same gate the add-domain form uses (security.md).
	exp.Domain = validate.NormalizeDomain(exp.Domain)
	if err := validate.Domain(exp.Domain); err != nil {
		h.renderBackupPage(w, r, http.StatusBadRequest, "Invalid domain in export file: "+err.Error())
		return
	}

	if msg := h.checkDomainImport(&exp); msg != "" {
		h.renderBackupPage(w, r, http.StatusBadRequest, msg)
		return
	}

	d, err := h.domains.Import(exp)
	if err != nil {
		logf("panel: import domain %q: %v", exp.Domain, err)
		status, msg := importErrorMessage(err)
		h.renderBackupPage(w, r, status, msg)
		return
	}
	if exp.DMARCRua != "" {
		h.resyncDMARC("domain import")
	}
	h.recalcImportedLimits(d, exp)
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?imported=1", d.ID), http.StatusSeeOther)
}

// secretFilePassword reads the "encrypt this download with a password" controls
// shared by the full-backup and domain-export forms. It returns the password to
// encrypt with — empty when the box is not ticked, which keeps the plain
// .tar.gz/.json behaviour of earlier versions — or a message to show above the
// form. The confirmation field is checked here rather than in the browser
// because a typo in an encryption password is unrecoverable: the archive would
// be sealed with a secret the operator does not know.
// reauth checks the account password a secret download asks for
// (backup-reauth): the full backup holds the whole database - the panel's
// password hashes, every application's SASL password, the DKIM keys - and a
// domain's export its key and its applications' credentials, so a session
// alone, stolen or left open, must not be enough to take them. The password
// is the signed-in user's own, checked as a password change checks it and
// under the same limiter (auth.AllowPasswordChange), so the form is no place
// to guess one. It returns 0 when it holds, else the status and the message
// for the form. what names the download in the log.
func (h *Handlers) reauth(r *http.Request, what string) (int, string) {
	p, ok := h.principal(r)
	if !ok {
		return http.StatusUnauthorized, "Sign in again."
	}
	if !h.auth.AllowPasswordChange(p.ID) {
		return http.StatusTooManyRequests, "Too many attempts. Please wait and try again."
	}
	if err := r.ParseForm(); err != nil {
		return http.StatusBadRequest, "Invalid form submission."
	}
	u, err := h.store.GetUser(p.ID)
	if err != nil {
		logf("panel: %s: get user %d: %v", what, p.ID, err)
		return http.StatusInternalServerError, "Internal error. Please try again."
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.PostFormValue("account_password"))); err != nil {
		logf("panel: %s: user %d gave a wrong account password", what, p.ID)
		return http.StatusUnauthorized, "Your account password is incorrect."
	}
	h.auth.PasswordCheckPassed(p.ID)
	return 0, ""
}

func secretFilePassword(r *http.Request) (password, errMsg string) {
	if err := r.ParseForm(); err != nil {
		return "", "Invalid form submission."
	}
	if r.PostFormValue("encrypt") == "" {
		return "", ""
	}
	password = r.PostFormValue("password")
	if len([]rune(password)) < validate.MinSecretFilePasswordLen {
		return "", fmt.Sprintf("The encryption password must be at least %d characters.", validate.MinSecretFilePasswordLen)
	}
	if password != r.PostFormValue("password_confirm") {
		return "", "The two passwords do not match."
	}
	return password, ""
}

// decryptErrorMessage phrases an envelope failure for the operator. A wrong
// password and a damaged file are deliberately indistinguishable to the code,
// so the message names both possibilities.
func decryptErrorMessage(err error) string {
	switch {
	case errors.Is(err, secretfile.ErrWrongPassword):
		return "Wrong password, or the file has been altered since it was exported."
	case errors.Is(err, secretfile.ErrCorrupt):
		return "That file is damaged or incomplete."
	case errors.Is(err, secretfile.ErrNotEncrypted):
		return "That file is not a SelfPost export."
	default:
		return "Could not decrypt the file."
	}
}

// unreadableBackupErr phrases what backup.Check found. The usual case is the
// private key: the panel runs as its own user, and a key readable by root
// alone is not its to read. The way out is on the host, so the message says
// what to do there (guide § Taking a full backup).
func unreadableBackupErr(err error, deployRoot string) string {
	if name, ok := backup.UnreadableCert(err, deployRoot); ok {
		return "The panel is not allowed to read " + name + ", and a backup without it would not bring the server back. " +
			"On the host, give the file to group 10997 and make it group-readable (chgrp 10997, chmod 640), then try again. Nothing was downloaded."
	}
	if errors.Is(err, backup.ErrNoCerts) {
		return "The project's certs/ holds no certificate, so a backup would bring the server back without one. " +
			"On the host, put fullchain.pem and privkey.pem - or links to certbot's - in certs/, then try again. Nothing was downloaded."
	}
	return "Could not read everything a backup holds. Nothing was downloaded; the reason is in the panel's log."
}

// deployBackupErr phrases a pre-flight backup failure for the operator.
func deployBackupErr(err error) string {
	if strings.Contains(err.Error(), "DeployRoot") || strings.Contains(err.Error(), "deploy root") {
		return "Full backup needs the project directory mounted read-only at /selfpost-deploy — add .:/selfpost-deploy:ro to the volumes in docker-compose.yml and recreate the container."
	}
	return "Could not create the backup: " + err.Error()
}

// importErrorMessage maps a domain-import failure (already logged by the caller)
// to an HTTP status and a user-facing message. Duplicate domain/login are called
// out specifically; other failures — validation errors describing what is wrong
// with the file, or an internal write/reload problem — are surfaced verbatim to
// this admin-only panel so the operator can act on them.
func importErrorMessage(err error) (int, string) {
	switch {
	case errors.Is(err, store.ErrDomainExists):
		return http.StatusConflict, "A domain with that name already exists here. Delete it first, or import into a fresh instance."
	case errors.Is(err, store.ErrLoginExists):
		return http.StatusConflict, "One of the application logins in the file is already in use on this instance. Application logins must be unique across all domains."
	default:
		return http.StatusBadRequest, "Could not import the domain: " + err.Error()
	}
}

// checkDomainImport holds an export file to the rules the forms hold the same
// settings to - the report address, the rate limits, the client addresses -
// before anything is created, and says what is wrong in the validator's own
// words; "" when nothing is. The file is untrusted input: a window of no
// seconds made a limit that counted nothing, and an address that is not one
// made an application refuse every client without a word at import (audit
// 2026-10-12, M10). Client addresses are written back as the form would store
// them. The login, the addresses an application may send as and the password
// are checked where the application is created (app.ImportApplication).
func (h *Handlers) checkDomainImport(exp *domain.DomainExport) string {
	invalid := func(what string, err error) string {
		return "Invalid " + what + " in export file: " + err.Error()
	}
	if err := validate.Selector(exp.DKIMSelector); err != nil {
		return invalid("DKIM selector", err)
	}
	if exp.DMARCRua != "" {
		if err := validate.Email(exp.DMARCRua); err != nil {
			return invalid("report address", err)
		}
		// As on Domain settings: an address on this server's own name is
		// only ever the domain's own hosted one (security audit 2026-10-17,
		// W1; the review of stage 1 found the import path open).
		if dmarc.IsHostedOnHostname(exp.DMARCRua, h.cfg.Hostname) &&
			!strings.EqualFold(strings.TrimSpace(exp.DMARCRua), dmarc.HostedReportAddress(h.cfg.Hostname, exp.Domain)) {
			return "The export's report address is on this server's own name and is not the domain's own hosted address. Edit the file's dmarc_rua, or set it to empty and choose the address on Domain settings after the import."
		}
	}
	limit := func(rl *domain.RateLimitExport) error {
		if rl == nil {
			return nil
		}
		mode, err := parseRateLimitMode(rl.Mode)
		if err != nil {
			return err
		}
		rl.Mode = mode // as the form stores it, trimmed
		if mode == store.RateLimitModeAuto {
			// An auto limit's numbers are this server's to work out from its
			// own traffic, not the file's: they are dropped here and worked
			// out right after the import (review of audit stage 2).
			rl.MaxMessages, rl.WindowSeconds = 0, 0
			if rl.AutoMultiplier == 0 {
				return nil // as an empty field on the form: the default
			}
			return checkAutoMultiplier(rl.AutoMultiplier)
		}
		if rl.MaxMessages <= 0 && rl.WindowSeconds <= 0 {
			return nil // no limit of its own (an export of client addresses only)
		}
		return checkManualLimit(rl.MaxMessages, rl.WindowSeconds, h.l1Messages())
	}
	if err := limit(exp.RateLimit); err != nil {
		return invalid("rate limit for the domain", err)
	}
	for i := range exp.Applications {
		a := &exp.Applications[i]
		if err := limit(a.RateLimit); err != nil {
			return invalid("rate limit for "+a.Login, err)
		}
		ips, err := parseIPList(strings.Join(a.AuthAllowedIPs, ","))
		if err != nil {
			return invalid("client addresses for "+a.Login, err)
		}
		a.AuthAllowedIPs = ips
		if a.RateLimit != nil && len(a.RateLimit.AllowedIPs) > 0 {
			legacy, err := parseIPList(strings.Join(a.RateLimit.AllowedIPs, ","))
			if err != nil {
				return invalid("client addresses for "+a.Login, err)
			}
			a.RateLimit.AllowedIPs = legacy
		}
		if a.AuthIPRestrict && len(a.AuthAllowedIPs) == 0 && (a.RateLimit == nil || len(a.RateLimit.AllowedIPs) == 0) {
			return invalid("client addresses for "+a.Login, errors.New("enter at least one client IP when the allow-list is enabled"))
		}
	}
	return ""
}

// recalcImportedLimits works out the numbers of the auto limits an import
// brought, at once rather than at the next six-hourly pass: the file's own
// numbers were dropped (checkDomainImport). A failure is logged; the pass
// tries again.
func (h *Handlers) recalcImportedLimits(d store.Domain, exp domain.DomainExport) {
	if exp.RateLimit != nil && exp.RateLimit.Mode == store.RateLimitModeAuto {
		if err := h.recalcRateLimit(store.RateLimitScopeDomain, d.ID); err != nil {
			logf("panel: import domain %q: recalculate its auto limit: %v", d.Name, err)
		}
	}
	for _, a := range exp.Applications {
		if a.RateLimit == nil || a.RateLimit.Mode != store.RateLimitModeAuto {
			continue
		}
		app, err := h.store.GetApplicationByLogin(a.Login)
		if err == nil {
			err = h.recalcRateLimit(store.RateLimitScopeApp, app.ID)
		}
		if err != nil {
			logf("panel: import domain %q: recalculate the auto limit of %s: %v", d.Name, a.Login, err)
		}
	}
}
