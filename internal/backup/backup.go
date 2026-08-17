// Package backup implements SelfPost's full-server backup and the restore
// version guard (architecture.md § Persistence). A full backup is a
// gzip-compressed tar of the consolidated persistent state under /data — the
// SQLite database (as a consistent snapshot), the per-domain DKIM keys, the
// SASL database, and the Postfix queue — plus docker-compose.yml, .env, and
// certs/ from the operator's deploy directory, and a manifest recording the
// SelfPost version that produced it. Postfix delivery logs under log/ are
// excluded (diagnostics, not state).
//
// Restore is not a separate code path in the panel: a backup is extracted into
// the operator's project directory (data/, docker-compose.yml, .env, certs/)
// before first start. The archive carries everything needed to bring the
// instance back — DKIM keys, sasldb2, sender map, queue, and deploy files —
// so the operator only adjusts hostname or proxy settings on a new host. The
// restore-specific steps the panel runs are CheckRestore, which refuses to
// boot if the manifest's version does not match the running binary so
// schema/format skew between versions cannot silently corrupt state
// (architecture.md § Persistence), and a one-time Resync of OpenDKIM's tables
// and the Postfix sender map from SQLite on that first boot, so any drift
// between the archive and the database is healed before mail flows. If
// on-disk state drifts again later — for example after a manual edit under
// /data — the Status page's "Reload configuration" button runs the same
// Resync on demand.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, for the VACUUM INTO snapshot
)

// FormatFull identifies a full-server backup manifest.
const FormatFull = "selfpost-full-backup"

// ManifestName is the manifest's filename inside the data directory. After
// restore extraction it lives at data/manifest.json under the project root;
// CheckRestore reads it from the /data bind mount.
const ManifestName = "manifest.json"

// DataArchivePrefix is the path prefix for every /data entry in the archive.
const DataArchivePrefix = "data/"

// ComposeFileName and EnvFileName are required deploy files at the archive root.
const (
	ComposeFileName = "docker-compose.yml"
	EnvFileName     = ".env"
	CertsDirName    = "certs"
)

// Manifest is the small JSON document embedded in every backup archive. Its
// Version is the single fact that makes restore safe: the panel refuses to
// boot a data directory whose manifest version does not match its own binary
// (architecture.md § Persistence).
type Manifest struct {
	Format    string `json:"format"`
	Version   string `json:"version"`
	CreatedAt string `json:"created_at"`
}

// Params configures a backup. DataDir is the consolidated state root (/data);
// DBPath is the live SQLite file within it, snapshotted consistently rather than
// copied byte-for-byte while it may be mid-write; Version is stamped into the
// manifest; DeployRoot is the host project directory mounted read-only (holds
// docker-compose.yml, .env, and optionally certs/). OnWarn is called for
// non-fatal issues such as a missing certs/ directory.
type Params struct {
	DataDir    string
	DBPath     string
	Version    string
	DeployRoot string
	OnWarn     func(string)
}

// excludedFromArchive lists the data-directory entries a backup never carries.
// The live database files are replaced by a consistent VACUUM INTO snapshot
// written under the canonical name; the setup token is transient bootstrap
// state; a stale manifest from a previous restore must not be re-captured (a
// fresh one is written instead); a "tls" directory under /data is skipped when
// an operator pointed TLS_CERT_FILE inside /data; and "log" is Postfix's raw
// delivery log plus its fourteen rotated files, which is diagnostic output, not
// state to restore, and by far the largest thing under /data.
var excludedFromArchive = map[string]bool{
	"selfpost.db":         true,
	"selfpost.db-wal":     true,
	"selfpost.db-shm":     true,
	"selfpost.db-journal": true,
	"setup-token":         true,
	"tls":                 true,
	"log":                 true,
	ManifestName:          true,
}

// Create writes a gzip-compressed tar backup to w. Archive layout:
//
//	data/manifest.json, data/selfpost.db, data/<rest of /data>
//	docker-compose.yml, .env, certs/...
//
// Extract the archive into an empty project directory, then docker compose up.
func Create(w io.Writer, p Params) error {
	if p.DataDir == "" || p.DBPath == "" {
		return fmt.Errorf("backup: DataDir and DBPath are required")
	}
	if p.DeployRoot == "" {
		return fmt.Errorf("backup: DeployRoot is required (mount the project directory at SELFPOST_DEPLOY_ROOT)")
	}
	if err := validateDeployRoot(p.DeployRoot); err != nil {
		return err
	}

	snapshot, cleanup, err := snapshotDB(p.DBPath)
	if err != nil {
		return err
	}
	defer cleanup()

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	manifest := Manifest{
		Format:    FormatFull,
		Version:   p.Version,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	if err := writeTarBytes(tw, DataArchivePrefix+ManifestName, 0o600, manifestJSON); err != nil {
		return err
	}

	// The consistent SQLite snapshot, under the canonical filename the panel
	// opens on start (the live file and its WAL/SHM are excluded from the walk).
	if err := writeTarFile(tw, DataArchivePrefix+"selfpost.db", 0o640, snapshot); err != nil {
		return err
	}

	if err := addTree(tw, p.DataDir, DataArchivePrefix, excludedFromArchive); err != nil {
		return err
	}

	if err := addDeployFiles(tw, p.DeployRoot, p.OnWarn); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("backup: close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("backup: close gzip: %w", err)
	}
	return nil
}

// ValidateDeployRoot checks that the operator project directory is mounted and
// contains the files a full backup requires. Call before streaming a response.
func ValidateDeployRoot(deployRoot string) error {
	if deployRoot == "" {
		return fmt.Errorf("backup: DeployRoot is required (mount the project directory at SELFPOST_DEPLOY_ROOT)")
	}
	return validateDeployRoot(deployRoot)
}

func validateDeployRoot(deployRoot string) error {
	for _, name := range []string{ComposeFileName, EnvFileName} {
		path := filepath.Join(deployRoot, name)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("backup: deploy root %q is missing %s: %w", deployRoot, name, err)
		}
	}
	return nil
}

func addDeployFiles(tw *tar.Writer, deployRoot string, onWarn func(string)) error {
	for _, name := range []string{ComposeFileName, EnvFileName} {
		src := filepath.Join(deployRoot, name)
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("backup: stat deploy file %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup: deploy file %s is not a regular file", name)
		}
		if err := writeTarFile(tw, name, info.Mode().Perm(), src); err != nil {
			return err
		}
	}

	certsDir := filepath.Join(deployRoot, CertsDirName)
	if _, err := os.Stat(certsDir); err != nil {
		if os.IsNotExist(err) {
			if onWarn != nil {
				onWarn("certs/ not found in deploy root; backup will not include TLS material")
			}
			return nil
		}
		return fmt.Errorf("backup: stat %s: %w", CertsDirName, err)
	}
	return addTree(tw, certsDir, CertsDirName+"/", nil)
}

// addTree walks root and adds every regular file (and directory, to preserve
// empty ones and modes) to tw under archivePrefix + path relative to root.
// When exclude is non-nil, top-level names relative to root are skipped.
// Non-regular, non-directory entries (symlinks, sockets) are skipped.
func addTree(tw *tar.Writer, root, archivePrefix string, exclude map[string]bool) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := archivePrefix + filepath.ToSlash(rel)
		if exclude != nil {
			top := strings.Split(filepath.ToSlash(rel), "/")[0]
			if exclude[top] {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     name + "/",
				Mode:     int64(info.Mode().Perm()),
				ModTime:  info.ModTime(),
			}
			return tw.WriteHeader(hdr)
		case info.Mode().IsRegular():
			return writeTarFile(tw, name, info.Mode().Perm(), path)
		default:
			return nil
		}
	})
}

// writeTarBytes writes an in-memory file entry.
func writeTarBytes(tw *tar.Writer, name string, mode int64, data []byte) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     mode,
		Size:     int64(len(data)),
		ModTime:  time.Now().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	return nil
}

// writeTarFile streams a file from disk into the archive under name.
func writeTarFile(tw *tar.Writer, name string, mode fs.FileMode, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", srcPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: stat %s: %w", srcPath, err)
	}
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     int64(mode.Perm()),
		Size:     info.Size(),
		ModTime:  info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write header %s: %w", name, err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("backup: copy %s: %w", name, err)
	}
	return nil
}

// snapshotDB produces a consistent copy of the SQLite database at dbPath using
// VACUUM INTO, so the backup captures a coherent point-in-time image even while
// the panel is writing to the live file under WAL. It returns the snapshot path
// and a cleanup function the caller must defer.
func snapshotDB(dbPath string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "selfpost-backup-")
	if err != nil {
		return "", nil, fmt.Errorf("backup: temp dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	target := filepath.Join(dir, "selfpost.db")

	// A short busy timeout lets VACUUM INTO wait out a brief writer rather than
	// failing immediately if the panel happens to be mid-write.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("backup: open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// VACUUM INTO takes a string literal, not a bound parameter. target is a path
	// we generated (never user input); single quotes are doubled defensively.
	stmt := "VACUUM INTO '" + strings.ReplaceAll(target, "'", "''") + "'"
	if _, err := db.Exec(stmt); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("backup: snapshot database: %w", err)
	}
	return target, cleanup, nil
}

// CheckRestore enforces the backup version guard (architecture.md §
// Persistence). If manifestPath exists (a backup was extracted into the data
// directory), its version must match binaryVersion or the panel refuses to
// start, telling the operator which image tag to use. On a match the manifest
// is consumed (deleted) so it guards only the first boot after a restore and
// never blocks a later in-place image upgrade, and restored is true so the
// caller can heal drifted daemon maps once. Absence of the manifest is the
// normal case and returns restored == false with a nil error.
func CheckRestore(manifestPath, binaryVersion string) (restored bool, err error) {
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return false, nil // ordinary start, not a restore
	}
	if err != nil {
		return false, fmt.Errorf("backup: read restore manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return false, fmt.Errorf("backup: restore manifest %s is not valid JSON: %w", manifestPath, err)
	}
	if m.Format != FormatFull {
		return false, fmt.Errorf("backup: %s is not a SelfPost full backup manifest (format %q)", manifestPath, m.Format)
	}
	if m.Version != binaryVersion {
		return false, fmt.Errorf(
			"backup: this backup was created by SelfPost %s but this image is %s — restore into the matching image (selfpost:%s)",
			m.Version, binaryVersion, m.Version)
	}
	// Version matches: consume the manifest so subsequent normal starts (and
	// in-place upgrades) are not gated by it.
	if err := os.Remove(manifestPath); err != nil {
		return false, fmt.Errorf("backup: consume restore manifest: %w", err)
	}
	return true, nil
}
