package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/secretfile"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
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
	data := h.pageBase(r)
	data["Title"] = "SelfPost — backup & migration"
	data["Active"] = "backup"
	data["ImportErr"] = importErr
	data["BackupErr"] = backupErr
	data["MinPwLen"] = validate.MinSecretFilePasswordLen
	h.view.Render(w, status, "backup", data)
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
	password, pwErr := secretFilePassword(r)
	if pwErr != "" {
		h.renderBackupPageWith(w, r, http.StatusBadRequest, "", pwErr)
		return
	}
	if err := backup.ValidateDeployRoot(h.cfg.DeployRoot); err != nil {
		logf("panel: full backup: %v", err)
		h.renderBackupPageWith(w, r, http.StatusBadRequest, "", deployBackupErr(err))
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

	if err := backup.Create(sink, backup.Params{
		DataDir:    h.cfg.DataDir,
		DBPath:     h.cfg.DBPath,
		Version:    h.cfg.Version,
		DeployRoot: h.cfg.DeployRoot,
		OnWarn: func(msg string) {
			logf("panel: full backup: %s", msg)
		},
	}); err != nil {
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
	password, pwErr := secretFilePassword(r)
	if pwErr != "" {
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormMode:  store.AddressModeWildcard,
			ExportErr: pwErr,
		})
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

	d, err := h.domains.Import(exp)
	if err != nil {
		logf("panel: import domain %q: %v", exp.Domain, err)
		status, msg := importErrorMessage(err)
		h.renderBackupPage(w, r, status, msg)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?imported=1", d.ID), http.StatusSeeOther)
}

// secretFilePassword reads the "encrypt this download with a password" controls
// shared by the full-backup and domain-export forms. It returns the password to
// encrypt with — empty when the box is not ticked, which keeps the plain
// .tar.gz/.json behaviour of earlier versions — or a message to show above the
// form. The confirmation field is checked here rather than in the browser
// because a typo in an encryption password is unrecoverable: the archive would
// be sealed with a secret the operator does not know.
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

// deployBackupErr phrases a pre-flight backup failure for the operator.
func deployBackupErr(err error) string {
	if strings.Contains(err.Error(), "DeployRoot") || strings.Contains(err.Error(), "deploy root") {
		return "Full backup needs the project directory mounted read-only at /selfpost-deploy — add <code>.:/selfpost-deploy:ro</code> to docker-compose.yml and recreate the container."
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
