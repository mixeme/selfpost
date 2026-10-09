package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrDomainExists is returned by AddDomain when the domain is already managed.
var ErrDomainExists = errors.New("domain already exists")

// ErrDomainNotFound is returned when a domain id/name does not exist.
var ErrDomainNotFound = errors.New("domain not found")

// Domain is a sending domain managed through the panel (product.md §
// Multi-domain model). The DKIM key material itself lives on disk under /data;
// this row records the selector and metadata. AppCount is populated by the
// listing queries, not stored.
type Domain struct {
	ID           int64
	Name         string
	DKIMSelector string
	// Where DMARC aggregate reports go. With DMARCRuaUserID set the domain
	// follows that user's default (DMARCDefault) and DMARCRua is ignored;
	// otherwise DMARCRua is the address itself, "" meaning no reports.
	DMARCRua       string
	DMARCRuaUserID sql.NullInt64
	CreatedAt      time.Time
	AppCount       int
}

// AddDomain inserts a new sending domain. The caller is responsible for having
// validated name (security.md) before it reaches SQL; the query is parameterised
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

// ListDomains returns every domain with its bound-application count (product.md),
// ordered by name.
func (s *Store) ListDomains() ([]Domain, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.name, d.dkim_selector, d.dmarc_rua, d.dmarc_rua_user_id, d.created_at,
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

// ListDomainsForUser returns domains assigned to userID with application counts,
// ordered by name.
func (s *Store) ListDomainsForUser(userID int64) ([]Domain, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.name, d.dkim_selector, d.dmarc_rua, d.dmarc_rua_user_id, d.created_at,
		       (SELECT COUNT(*) FROM applications a WHERE a.domain_id = d.id)
		FROM domains d
		INNER JOIN user_domains ud ON ud.domain_id = d.id
		WHERE ud.user_id = ?
		ORDER BY d.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list domains for user: %w", err)
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
		SELECT d.id, d.name, d.dkim_selector, d.dmarc_rua, d.dmarc_rua_user_id, d.created_at,
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
// go with it via ON DELETE CASCADE (product.md). Returns ErrDomainNotFound if no
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
	if err := r.Scan(&d.ID, &d.Name, &d.DKIMSelector, &d.DMARCRua, &d.DMARCRuaUserID, &createdAt, &d.AppCount); err != nil {
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

// SetDomainDMARCAddress gives the domain a report address of its own; "" means
// no aggregate reports. The domain stops following anyone's default.
func (s *Store) SetDomainDMARCAddress(id int64, rua string) error {
	return s.updateDomainDMARC(id, rua, sql.NullInt64{})
}

// SetDomainDMARCUser makes the domain follow userID's default report address
// (DMARCDefault). If that user is later deleted the domain falls back to no
// reports rather than to someone else's default.
func (s *Store) SetDomainDMARCUser(id, userID int64) error {
	return s.updateDomainDMARC(id, "", sql.NullInt64{Int64: userID, Valid: true})
}

func (s *Store) updateDomainDMARC(id int64, rua string, userID sql.NullInt64) error {
	res, err := s.db.Exec("UPDATE domains SET dmarc_rua = ?, dmarc_rua_user_id = ? WHERE id = ?", rua, userID, id)
	if err != nil {
		return fmt.Errorf("update domain dmarc rua: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update domain dmarc rua rows: %w", err)
	}
	if n == 0 {
		return ErrDomainNotFound
	}
	return nil
}

// DomainDMARCRua resolves where the domain's aggregate reports go: its own
// address, or the default of the user it follows. hosted is the SelfPost-hosted
// mailbox for this domain, "" when report ingest is off (the store does not
// know the server's hostname). The result is "" for a policy-only record.
func (s *Store) DomainDMARCRua(d Domain, hosted string) (string, error) {
	if !d.DMARCRuaUserID.Valid {
		return d.DMARCRua, nil
	}
	def, err := s.GetDMARCDefault(d.DMARCRuaUserID.Int64)
	if errors.Is(err, ErrUserNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return def.Resolve(hosted), nil
}

// DomainExists reports whether a sending domain with this name is configured.
// Used by the DMARC ingest path to refuse reports for domains this relay does
// not send for (security.md: report XML is attacker-supplied input).
func (s *Store) DomainExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM domains WHERE name = ? COLLATE NOCASE", strings.TrimSpace(name),
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("domain exists %q: %w", name, err)
	}
	return n > 0, nil
}
