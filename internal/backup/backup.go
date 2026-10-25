// Package backup implements SelfPost's full-server backup and the restore
// version guard (architecture.md § Persistence). A full backup is a
// gzip-compressed tar of the consolidated persistent state under /data — the
// SQLite database (as a consistent snapshot), the per-domain DKIM keys and the
// SASL database — plus docker-compose.yml, .env, and certs/ from the
// operator's deploy directory, and a manifest recording the SelfPost version
// that produced it. Postfix delivery logs under log/ are excluded
// (diagnostics, not state), and so is the Postfix queue (see QueueDir).
//
// Restore is not a separate code path in the panel: a backup is extracted into
// the operator's project directory (data/, docker-compose.yml, .env, certs/)
// before first start. The archive carries everything needed to bring the
// instance back — DKIM keys, sasldb2, sender map, and deploy files —
// so the operator only adjusts hostname or proxy settings on a new host. The
// restore-specific steps the panel runs are CheckRestore, which refuses to
// boot if the manifest's version does not match the running binary so
// schema/format skew between versions cannot silently corrupt state
// (architecture.md § Persistence), or if the archive was unpacked in part
// (ContentsName), and a one-time Resync of the DKIM selector table
// and the Postfix sender map from SQLite on that first boot, so any drift
// between the archive and the database is healed before mail flows. If
// on-disk state drifts again later — for example after a manual edit under
// /data — the Status page's "Reload configuration" button runs the same
// Resync on demand.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
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

// QueueDir is the Postfix queue, relative to the data directory. No backup
// holds it (owner, 2026-10-11). An archive is made while the instance runs,
// and Postfix moves messages between the queue's directories all the time: a
// copy taken then can hold a message twice or not at all, so what it would
// bring back is not the queue. Nor could the panel read it - Postfix keeps
// every queue file readable by its own user alone, which is why the panel's
// backup failed outright from 1.3.0 to 2.0.0 (date not recorded; Postfix
// 3.10.13, conf/postfix-files: active, deferred, incoming and the other queue
// directories are $mail_owner 700). What is not carried over is mail
// still waiting for delivery; a start without a queue directory builds an
// empty one. Whoever needs the queue stops the instance and archives the
// project directory (guide § Taking a full backup).
const QueueDir = "postfix/queue"

// excludedFromArchive lists the data-directory entries a backup never carries.
// The live database files are replaced by a consistent VACUUM INTO snapshot
// written under the canonical name; the setup token is transient bootstrap
// state; a stale manifest from a previous restore must not be re-captured (a
// fresh one is written instead); a "tls" directory under /data is skipped when
// an operator pointed TLS_CERT_FILE inside /data; and "log" is Postfix's raw
// delivery log plus its fourteen rotated files, which is diagnostic output, not
// state to restore, and by far the largest thing under /data. (Until 2.9.0
// "opendkim" was left out too: the key tree OpenDKIM read until 2.8.0, kept on
// disk for a rollback to 2.7.x; the entrypoint removes it before the panel
// starts since.)
var excludedFromArchive = map[string]bool{
	"selfpost.db":         true,
	"selfpost.db-wal":     true,
	"selfpost.db-shm":     true,
	"selfpost.db-journal": true,
	"setup-token":         true,
	"tls":                 true,
	"log":                 true,
	ManifestName:          true,
	ContentsName:          true,
	contentsCheckedName:   true,
}

// Create writes a gzip-compressed tar backup to w. Archive layout:
//
//	data/manifest.json, data/selfpost.db, data/<rest of /data>
//	docker-compose.yml, .env, certs/...
//	data/contents.json
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
	a := &archive{tw: tw}

	manifest := Manifest{
		Format:    FormatFull,
		Version:   p.Version,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	if err := writeTarBytes(a, DataArchivePrefix+ManifestName, 0o600, manifestJSON); err != nil {
		return err
	}

	// The consistent SQLite snapshot, under the canonical filename the panel
	// opens on start (the live file and its WAL/SHM are excluded from the walk).
	if err := writeTarFile(a, DataArchivePrefix+"selfpost.db", 0o640, snapshot); err != nil {
		return err
	}

	if _, err := addTree(a, p.DataDir, DataArchivePrefix, excludedFromArchive, skipQueue, false, p.OnWarn); err != nil {
		return err
	}

	if err := addDeployFiles(a, p.DeployRoot, p.OnWarn); err != nil {
		return err
	}

	// Last, so that only an archive unpacked to its end has it.
	if err := a.writeContents(); err != nil {
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

// skipFunc says whether the entry at path (rel to the root being walked) is
// left out of the archive. A directory left out is not descended into.
type skipFunc func(path, rel string, d fs.DirEntry) bool

// skipQueue leaves the Postfix queue out of every archive, and the copy of
// sasldb2 a change is made on (internal/app, replaceSASLDB): a panel killed
// mid-change leaves it, a second copy of every application's password, until
// the next start removes it (build/entrypoint.sh; review of stage 2c).
func skipQueue(_, rel string, _ fs.DirEntry) bool {
	switch filepath.ToSlash(rel) {
	case QueueDir, "sasl/sasldb2.new":
		return true
	}
	return false
}

// Check reads through everything Create would put into the archive, without
// writing one, and returns the first thing that cannot be read. The panel
// streams its archive to the browser, so once Create has begun a failure can
// only cut the download short; this is run first, while a refusal can still be
// shown as a page.
//
// The panel is not root, and nothing is left out on that account: its archive
// is the one selfpost-backup makes, or it is not made. What it most likely
// cannot read is the private key under certs/ - see UnreadableCert.
func Check(p Params) error {
	if p.DataDir == "" || p.DBPath == "" {
		return fmt.Errorf("backup: DataDir and DBPath are required")
	}
	if err := ValidateDeployRoot(p.DeployRoot); err != nil {
		return err
	}
	if _, err := addTree(nil, p.DataDir, DataArchivePrefix, excludedFromArchive, skipQueue, false, nil); err != nil {
		return err
	}
	return addDeployFiles(nil, p.DeployRoot, nil)
}

// UnreadableCert reports whether err, from Check or Create, is a file under
// the deploy root's certs/ that the caller is not allowed to read, and names
// it relative to the deploy root ("certs/privkey.pem").
func UnreadableCert(err error, deployRoot string) (name string, ok bool) {
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || !errors.Is(err, fs.ErrPermission) {
		return "", false
	}
	rel, relErr := filepath.Rel(filepath.Join(deployRoot, CertsDirName), pathErr.Path)
	if relErr != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return "", false
	}
	return CertsDirName + "/" + filepath.ToSlash(rel), true
}

func addDeployFiles(a *archive, deployRoot string, onWarn func(string)) error {
	for _, name := range []string{ComposeFileName, EnvFileName} {
		src := filepath.Join(deployRoot, name)
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("backup: stat deploy file %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup: deploy file %s is not a regular file", name)
		}
		if err := writeTarFile(a, name, info.Mode().Perm(), src); err != nil {
			return err
		}
	}

	certsDir := filepath.Join(deployRoot, CertsDirName)
	// certs/ itself may be a link - the guide once had it linked to
	// certbot's live/<host> - and the walk below does not follow its root
	// (path/filepath, WalkDir: "WalkDir does not follow symbolic links"), so
	// the link is resolved first (review of stage 2c, F7).
	if resolved, err := filepath.EvalSymlinks(certsDir); err == nil {
		certsDir = resolved
	}
	if _, err := os.Stat(certsDir); err != nil {
		if os.IsNotExist(err) {
			if onWarn != nil {
				onWarn("certs/ not found in deploy root; backup will not include TLS material")
			}
			return nil
		}
		return fmt.Errorf("backup: stat %s: %w", CertsDirName, err)
	}
	// certs/ may hold links rather than files: certbot's own advice is to
	// point a server at its live/ files "(or create symlinks)", and those
	// are links again, relative, into archive/ (eff-certbot docs, "Where are
	// my certificates?"; assumptions C-Q4, C-A1). The archive carries the
	// files they lead to, under the links' names.
	files, err := addTree(a, certsDir, CertsDirName+"/", nil, nil, true, onWarn)
	if err != nil {
		return err
	}
	// certs/ is what Postfix serves its certificate from (the compose file
	// mounts it at /etc/selfpost/tls). There and empty - or holding only what
	// the archive cannot carry - it would give an archive that restores a
	// server without its certificate, so there is none (audit 2026-10-12,
	// L2).
	if files == 0 {
		return ErrNoCerts
	}
	return nil
}

// ErrNoCerts is a certs/ that is there with no file a backup can carry.
var ErrNoCerts = errors.New("backup: " + CertsDirName + "/ holds no file this backup can carry, so the server it restores would have no certificate - put the PEM files (or links to them) there")

// addTree walks root and adds every regular file (and directory, to preserve
// empty ones and modes) to a under archivePrefix + path relative to root, and
// returns how many files it took.
// When exclude is non-nil, top-level names relative to root are skipped; skip,
// when non-nil, leaves out whatever it says. With a nil a nothing is written:
// every file is opened and closed, which is how Check finds what cannot be read.
// Non-regular, non-directory entries (symlinks, sockets) are skipped - except,
// with follow, a symbolic link to a regular file, which is archived as that
// file under the link's name; a link that leads nowhere this process can
// reach is an error, as an unreadable file is. Each entry left out that way
// is told to warn, when it is not nil: a backup does not leave something out
// without saying so (audit 2026-10-12, L2).
func addTree(a *archive, root, archivePrefix string, exclude map[string]bool, skip skipFunc, follow bool, warn func(string)) (files int, err error) {
	skipped := func(name, what string) {
		if warn != nil {
			warn(name + " is " + what + ", not a file: the backup does not carry it")
		}
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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

		if skip != nil && skip(path, rel, d) {
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
			if a == nil {
				return nil
			}
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     name + "/",
				Mode:     int64(info.Mode().Perm()),
				ModTime:  info.ModTime(),
			}
			return a.tw.WriteHeader(hdr)
		case info.Mode().IsRegular():
			err := writeTarFile(a, name, info.Mode().Perm(), path)
			if errors.Is(err, fs.ErrNotExist) {
				// A writer's temporary file, renamed into place between
				// the listing and the open: not part of /data's state.
				return nil
			}
			if err == nil {
				files++
			}
			return err
		case follow && info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("backup: %s is a link to a file this container cannot reach - with certbot, /etc/letsencrypt is mounted at the same path (guide § Reverse proxy): %w", name, err)
			}
			if !target.Mode().IsRegular() {
				skipped(name, "a link to a directory")
				return nil
			}
			if err := writeTarFile(a, name, target.Mode().Perm(), path); err != nil {
				return err
			}
			files++
			return nil
		case info.Mode()&fs.ModeSymlink != 0:
			skipped(name, "a link")
			return nil
		default:
			skipped(name, kindOf(info.Mode()))
			return nil
		}
	})
	return files, err
}

// kindOf names what a non-regular, non-directory entry is, for a warning.
func kindOf(m fs.FileMode) string {
	switch {
	case m&fs.ModeSocket != 0:
		return "a socket"
	case m&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case m&fs.ModeDevice != 0:
		return "a device"
	default:
		return "neither a file nor a directory"
	}
}

// writeTarBytes writes an in-memory file entry.
func writeTarBytes(a *archive, name string, mode int64, data []byte) error {
	return a.writeFile(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     mode,
		Size:     int64(len(data)),
		ModTime:  time.Now().UTC(),
	}, bytes.NewReader(data))
}

// writeTarFile streams a file from disk into the archive under name.
//
// The file is read through one descriptor, sized by that descriptor: the
// panel's writers of what a backup carries put a new file in place by rename
// (atomicfile.Write; sasldb2 through a copy, app.replaceSASLDB - the setup
// token alone is written in place, and it is not carried; the database and
// the queue are not walked), and a rename leaves the file open here as it
// was: the archive holds the old file or the new one, whole (assumptions
// C-Q3).
func writeTarFile(a *archive, name string, mode fs.FileMode, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", srcPath, err)
	}
	defer f.Close()
	if a == nil {
		return nil
	}

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: stat %s: %w", srcPath, err)
	}
	return a.writeFile(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     int64(mode.Perm()),
		Size:     info.Size(),
		ModTime:  info.ModTime(),
	}, f)
}

// snapshotDB produces a consistent copy of the SQLite database at dbPath using
// VACUUM INTO, so the backup captures a coherent point-in-time image even while
// the panel is writing to the live file under WAL. It returns the snapshot path
// and a cleanup function the caller must defer. (sqlite.org/lang_vacuum.html,
// "VACUUM INTO": "transactional in the sense that the generated output
// database is a consistent snapshot"; modernc.org/sqlite v1.60.1, sqlite.go,
// applyQueryParams(): each _pragma is run as "pragma <value>" on connect,
// busy_timeout first.)
func snapshotDB(dbPath string) (path string, cleanup func(), err error) {
	// The database must be there: SQLite creates a file that is not, and an
	// archive of that empty database restores a panel that offers its
	// first-run setup on a server holding live keys (audit 2026-10-12, M3).
	// mode=ro makes the open fail rather than create one, should the file go
	// between the two: SQLite 3.53.4 creates a database only under "rwc", the
	// default (https://sqlite.org/uri.html § 3.3), and VACUUM INTO is not a
	// write to the source (lang_vacuum.html § 2.1, § 3) - both measured by
	// TestSnapshotRefusesAMissingDatabase.
	info, err := os.Stat(dbPath)
	if err != nil {
		return "", nil, fmt.Errorf("backup: the database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("backup: the database %s is not a file", dbPath)
	}
	dir, err := os.MkdirTemp("", "selfpost-backup-")
	if err != nil {
		return "", nil, fmt.Errorf("backup: temp dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	target := filepath.Join(dir, "selfpost.db")

	// A short busy timeout lets VACUUM INTO wait out a brief writer rather than
	// failing immediately if the panel happens to be mid-write.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
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
// start, telling the operator which image tag to use; and the data directory
// must hold every file the archive recorded at its end, as the archive holds
// it, or the panel refuses to start on an archive unpacked in part (see
// ContentsName). Then restored is true so the caller can heal drifted daemon
// maps once. The manifest stays until the caller has done that and calls
// ConsumeRestore: a start that fails on the way leaves it, and the next start
// is a restore again (audit 2026-10-12, M1). Absence of the manifest is the
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
	if err := checkContents(filepath.Dir(manifestPath)); err != nil {
		return false, err
	}
	return true, nil
}

// ConsumeRestore ends a restore once its first start has done what a restore
// start does: the manifest is removed, so later starts (and in-place
// upgrades) are not gated by it, and then the record of the files, checked or
// unpacked again since. A start stopped between the two leaves the record
// alone, which nothing reads without a manifest and no backup carries.
func ConsumeRestore(manifestPath string) error {
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backup: consume restore manifest: %w", err)
	}
	for _, name := range []string{contentsCheckedName, ContentsName} {
		err := os.Remove(filepath.Join(filepath.Dir(manifestPath), name))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("backup: consume restore manifest: %w", err)
		}
	}
	return nil
}
