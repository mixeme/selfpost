// Package store owns the SelfPost SQLite database: the single file under /data
// that persists panel users (the global and domain roles), sending
// domains and applications, optional inbound-relay domains, the send log and
// rate-limit settings (architecture.md § Persistence). It exposes typed queries
// so the rest of the panel never builds SQL by hand.
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"sync/atomic"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo), keeps the static build
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database connection pool.
type Store struct {
	db *sql.DB

	// inboundWrites counts journal rows written since the journal was last
	// cut to its row cap, by both of its writers (countInboundWrites).
	inboundWrites atomic.Int64
}

// Open opens (creating if needed) the SQLite database at path, enables WAL and
// foreign keys, and applies any pending migrations. The caller owns Close.
func Open(path string) (*Store, error) {
	// _pragma parameters are applied on every pooled connection by the driver,
	// so foreign-key enforcement and WAL survive connection churn.
	// modernc.org/sqlite v1.60.1, conn.go newConn() -> sqlite.go
	// applyQueryParams(): the _pragma list runs on every new connection,
	// busy_timeout first. SQLite 3.53.4: foreign_keys is per connection
	// (https://sqlite.org/foreignkeys.html § 2), journal_mode=WAL stays in the
	// file (https://sqlite.org/wal.html § 3.3).
	//
	// _txlock=immediate: every transaction that may write takes the write
	// lock at BEGIN (modernc.org/sqlite v1.60.1, sqlite.go applyQueryParams()
	// sets beginMode, tx.go newTx() issues "begin immediate"; read-only
	// transactions keep a plain begin). A deferred BEGIN starts nothing
	// until the first access (lang_transaction.html § 2.2), so two processes
	// - the panel and dmarc-ingest after an upgrade - could each read the
	// schema version and then both run the next migration; an immediate one
	// waits through busy_timeout, holding no lock that the other needs, and
	// migrate() reads the version again under it (assumptions D-S3).
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection: database/sql then runs this process's statements and
	// transactions one after another, so none of them meets another's lock.
	// The driver does not serialise across connections (modernc.org/sqlite
	// v1.60.1, conn.go: the conn's mutex is its own); between processes - the
	// dmarc-ingest command opens this file too - SQLite allows one write
	// transaction at a time (https://sqlite.org/lang_transaction.html § 2.1)
	// and the other waits out busy_timeout.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := keepPrivate(path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// keepPrivate makes the database file and its WAL and shared-memory files
// the panel's alone (0600). They hold the panel users' password hashes and
// the session token hashes, and /data is traversable by every user of the
// container (entrypoint.sh) - Postfix among them. SQLite
// creates a file with 0644 less the umask, and the panel sets no umask; the
// -wal and -shm files take the database file's mode when they are created
// (SQLite 3.53.4 as modernc.org/sqlite v1.60.1 builds it,
// lib/sqlite_linux_386.go: findCreateFileMode(), "WAL and journal files are
// created using the same permissions as the associated database file";
// unixOpenSharedMemory(), robust_open(... st_mode&0777) of the database's
// fstat). So the database file is chmodded and the two files beside it,
// where they exist already; the next ones follow its mode. Found by the
// security review of base-trixie (2026-10-15): the file was 0644 on the
// test server.
func keepPrivate(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("database mode: %w", err)
		}
	}
	return nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// applicationID marks a database file as a 2.x SelfPost one ("SP20"). The
// baseline migration sets it.
const applicationID = 0x53503230

// ErrForeignSchema is returned by Open for a database that has a schema but
// was not created by SelfPost 2.x — in practice a 1.x data directory.
var ErrForeignSchema = errors.New("database was not created by SelfPost 2.x; 2.0 starts from an empty data directory (docs/schema-migrations.md)")

// migrate applies embedded migrations in filename order, tracking progress via
// SQLite's PRAGMA user_version so each migration runs at most once: the
// version is read again inside each migration's transaction, which holds the
// write lock from its BEGIN (Open, _txlock), and a migration another process
// applied in the meantime is skipped.
func (s *Store) migrate() error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// A 1.x file carries a user_version from the deleted 1.x chain. Counting it
	// against today's files would skip the baseline and leave the panel running
	// on a schema it does not know, so such a file is refused outright; nothing
	// is migrated from 1.x (docs/schema-migrations.md).
	// SQLite 3.53.4: the application id is the header's integer at offset 68
	// (https://sqlite.org/fileformat2.html § 1.3.15); btree.c newDatabase()
	// zeroes bytes 24-99 of a new file's header, so it reads 0 until set - and
	// 1.x never set it.
	if version > 0 {
		var appID int64
		if err := s.db.QueryRow("PRAGMA application_id").Scan(&appID); err != nil {
			return fmt.Errorf("read application id: %w", err)
		}
		if appID != applicationID {
			return fmt.Errorf("schema version %d: %w", version, ErrForeignSchema)
		}
	}
	if version > len(names) {
		return fmt.Errorf("database schema version %d is newer than this build knows (%d)", version, len(names))
	}

	for i, name := range names {
		target := i + 1
		if target <= version {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		var now int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&now); err != nil {
			tx.Rollback()
			return fmt.Errorf("read schema version for %s: %w", name, err)
		}
		if now >= target {
			// Applied by another process while this one waited for the lock.
			tx.Rollback()
			continue
		}
		// One Exec runs the whole file: modernc.org/sqlite v1.60.1, stmt.go
		// stmt.exec(), "FALLBACK PATH: Multi-statement script" prepares and
		// steps each statement of the tail in turn. It runs with foreign_keys
		// ON, which cannot be switched off inside a transaction
		// (https://sqlite.org/pragma.html#pragma_foreign_keys): an ADD COLUMN
		// with REFERENCES needs a NULL default (lang_altertable.html § 4), and
		// a changed collation means a rebuilt table (§ 8).
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		// PRAGMA does not accept a bound parameter, and target is a trusted
		// loop index, so formatting it in is safe.
		// SQLite 3.53.4, pragma.c sqlite3Pragma(), PragTyp_HEADER_VALUE: the
		// value is the literal's text (sqlite3Atoi), written by OP_SetCookie
		// under a write transaction, so a rollback takes it back with the rest.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", target)); err != nil {
			tx.Rollback()
			return fmt.Errorf("bump schema version for %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}
