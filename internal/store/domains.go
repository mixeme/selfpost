package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrDomainExists is returned by AddDomain when the domain is already managed.
var ErrDomainExists = errors.New("domain already exists")

// ErrDomainNotFound is returned when a domain id/name does not exist.
var ErrDomainNotFound = errors.New("domain not found")

// Domain is a sending domain managed through the panel (spec 4.1). The DKIM key
// material itself lives on disk under /data; this row records the selector and
// metadata. AppCount is populated by the listing queries, not stored.
type Domain struct {
	ID           int64
	Name         string
	DKIMSelector string
	CreatedAt    time.Time
	AppCount     int
}

// AddDomain inserts a new sending domain. The caller is responsible for having
// validated name (spec 7.6.2) before it reaches SQL; the query is parameterised
// regardless. A duplicate name maps to ErrDomainExists.
func (s *Store) AddDomain(name, selector string) (Domain, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		"INSERT INTO domains (name, dkim_selector, created_at) VALUES (?, ?, ?)",
		name, selector, now.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Domain{}, ErrDomainExists
		}
		return Domain{}, fmt.Errorf("insert domain: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Domain{}, fmt.Errorf("domain id: %w", err)
	}
	return Domain{ID: id, Name: name, DKIMSelector: selector, CreatedAt: now}, nil
}

// ListDomains returns every domain with its bound-application count (spec 7.2.2),
// ordered by name.
func (s *Store) ListDomains() ([]Domain, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.name, d.dkim_selector, d.created_at,
		       (SELECT COUNT(*) FROM applications a WHERE a.domain_id = d.id)
		FROM domains d
		ORDER BY d.name`)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()

	var out []Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDomain returns a single domain (with its application count) by id, or
// ErrDomainNotFound.
func (s *Store) GetDomain(id int64) (Domain, error) {
	row := s.db.QueryRow(`
		SELECT d.id, d.name, d.dkim_selector, d.created_at,
		       (SELECT COUNT(*) FROM applications a WHERE a.domain_id = d.id)
		FROM domains d
		WHERE d.id = ?`, id)
	d, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrDomainNotFound
	}
	if err != nil {
		return Domain{}, err
	}
	return d, nil
}

// DeleteDomain removes a domain. Its applications and their address/binding rows
// go with it via ON DELETE CASCADE (spec 7.2.4). Returns ErrDomainNotFound if no
// such row existed.
func (s *Store) DeleteDomain(id int64) error {
	res, err := s.db.Exec("DELETE FROM domains WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete domain rows: %w", err)
	}
	if n == 0 {
		return ErrDomainNotFound
	}
	return nil
}

// scanRow is the minimal surface shared by *sql.Row and *sql.Rows.
type scanRow interface {
	Scan(dest ...any) error
}

func scanDomain(r scanRow) (Domain, error) {
	var (
		d         Domain
		createdAt string
	)
	if err := r.Scan(&d.ID, &d.Name, &d.DKIMSelector, &createdAt, &d.AppCount); err != nil {
		return Domain{}, err
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return d, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE/PRIMARY-KEY conflict,
// so callers can turn a duplicate insert into a friendly domain-level error.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}
