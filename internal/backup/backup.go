// Package backup implements SelfPost's full-server backup and the restore
// version guard (spec 7.5.A). A full backup is a gzip-compressed tar of the
// consolidated persistent state under /data — the SQLite database (as a
// consistent snapshot), the per-domain DKIM keys and the SASL database — plus a
// manifest recording the SelfPost version that produced it. TLS certificates
// (the reverse proxy's responsibility) and the Postfix queue are deliberately
// excluded (spec 7.5.A).
//
// Restore is intentionally not a separate code path: a backup is extracted into
// the /data bind mount before first start, and the panel regenerates Postfix and
// OpenDKIM from the restored SQLite state exactly as on any normal start. The
// only restore-specific step is CheckRestore, which refuses to boot if the
// manifest's version does not match the running binary, so schema/format skew
// between versions cannot silently corrupt state (spec 7.5.A).
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

// ManifestName is the manifest's filename, both inside the archive and, after a
// restore extraction, at the root of the data directory where CheckRestore
// looks for it.
const ManifestName = "manifest.json"

// Manifest is the small JSON document embedded in every backup archive. Its
// Version is the single fact that makes restore safe: the panel refuses to boot
// a data directory whose manifest version does not match its own binary (spec
// 7.5.A).
type Manifest struct {
	Format    string `json:"format"`
	Version   string `json:"version"`
	CreatedAt string `json:"created_at"`
}

// Params configures a backup. DataDir is the consolidated state root (/data);
// DBPath is the live SQLite file within it, snapshotted consistently rather than
// copied byte-for-byte while it may be mid-write; Version is stamped into the
// manifest.
type Params struct {
	DataDir string
	DBPath  string
	Version string
}

// excludedFromArchive lists the data-directory entries a backup never carries.
// The live database files are replaced by a consistent VACUUM INTO snapshot
// written under the canonical name; the setup token is transient bootstrap
// state; a stale manifest from a previous restore must not be re-captured (a
// fresh one is written instead); and a "tls" directory holds the reverse
// proxy's certificates, which are explicitly out of scope for a SelfPost backup
// (spec 7.5.A) — excluding it keeps that guarantee even when an operator points
// TLS_CERT_FILE inside /data.
var excludedFromArchive = map[string]bool{
	"selfpost.db":         true,
	"selfpost.db-wal":     true,
	"selfpost.db-shm":     true,
	"selfpost.db-journal": true,
	"setup-token":         true,
	"tls":                 true,
	ManifestName:          true,
}

// Create writes a gzip-compressed tar backup to w. Archive entries are named
// relative to DataDir, so extracting the archive into the /data bind mount
// reconstructs the state in place (spec 7.5.A). The SQLite database is added as
// a consistent snapshot under "selfpost.db"; everything else under DataDir is
// copied as-is except the entries in excludedFromArchive.
func Create(w io.Writer, p Params) error {
	if p.DataDir == "" || p.DBPath == "" {
		return fmt.Errorf("backup: DataDir and DBPath are required")
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
	if err := writeTarBytes(tw, ManifestName, 0o600, manifestJSON); err != nil {
		return err
	}

	// The consistent SQLite snapshot, under the canonical filename the panel
	// opens on start (the live file and its WAL/SHM are excluded from the walk).
	if err := writeTarFile(tw, "selfpost.db", 0o640, snapshot); err != nil {
		return err
	}

	if err := addTree(tw, p.DataDir); err != nil {
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

// addTree walks dataDir and adds every regular file (and directory, to preserve
// empty ones and modes) to tw under its path relative to dataDir, skipping the
// excluded entries. Non-regular, non-directory entries (symlinks, sockets) are
// skipped: /data holds none in normal operation, and copying them into a backup
// would be meaningless or unsafe.
func addTree(tw *tar.Writer, dataDir string) error {
	return filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the data root itself is implicit
		}
		// filepath.Rel yields OS separators; tar names use forward slashes.
		name := filepath.ToSlash(rel)
		// Exclude by top-level name (the live DB, setup token and stale manifest
		// all live at the data root).
		if excludedFromArchive[name] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
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
			return nil // skip symlinks/sockets/devices
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

// CheckRestore enforces the backup version guard (spec 7.5.A). If manifestPath
// exists (a backup was extracted into the data directory), its version must
// match binaryVersion or the panel refuses to start, telling the operator which
// image tag to use. On a match the manifest is consumed (deleted) so it guards
// only the first boot after a restore and never blocks a later in-place image
// upgrade. Absence of the manifest is the normal case and returns nil.
func CheckRestore(manifestPath, binaryVersion string) error {
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return nil // ordinary start, not a restore
	}
	if err != nil {
		return fmt.Errorf("backup: read restore manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("backup: restore manifest %s is not valid JSON: %w", manifestPath, err)
	}
	if m.Format != FormatFull {
		return fmt.Errorf("backup: %s is not a SelfPost full backup manifest (format %q)", manifestPath, m.Format)
	}
	if m.Version != binaryVersion {
		return fmt.Errorf(
			"backup: this backup was created by SelfPost %s but this image is %s — restore into the matching image (selfpost:%s)",
			m.Version, binaryVersion, m.Version)
	}
	// Version matches: consume the manifest so subsequent normal starts (and
	// in-place upgrades) are not gated by it.
	if err := os.Remove(manifestPath); err != nil {
		return fmt.Errorf("backup: consume restore manifest: %w", err)
	}
	return nil
}
