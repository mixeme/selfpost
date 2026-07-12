package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrLoginExists is returned when an application login is already taken. The
// login is globally unique because it is the SASL identity Postfix authenticates
// (one sasldb2 across all domains, spec 5.1).
var ErrLoginExists = errors.New("application login already exists")

// ErrApplicationNotFound is returned when an application id does not exist.
var ErrApplicationNotFound = errors.New("application not found")

// Address modes (spec 4.1). Kept in sync with the CHECK constraint in the schema.
const (
	AddressModeWildcard = "wildcard" // any address within the application's domain
	AddressModeList     = "list"     // only the explicitly listed addresses
)

// Application is a SASL account bound to a single domain (spec 4.1, 5.1). The
// password is never stored here — only in sasldb2, hashed — so it can be shown
// exactly once at creation/regeneration (spec 7.6.1). Addresses is populated only
// in 'list' mode.
type Application struct {
	ID          int64
	DomainID    int64
	Login       string
	AddressMode string
	CreatedAt   time.Time
	Addresses   []string
}

// Binding is one sender-address → login pair, as consumed by the
// smtpd_sender_login_maps generator (spec 5.1). For a wildcard application the
// Address is the domain wildcard "@example.com"; for a list application there is
// one Binding per listed address.
type Binding struct {
	Address string
	Login   string
}

// AddApplication inserts an application and, in list mode, its addresses, in a
// single transaction. The caller must have validated login and every address
// (spec 7.6.2) beforehand; the query is parameterised regardless. A duplicate
// login maps to ErrLoginExists.
func (s *Store) AddApplication(domainID int64, login, mode string, addresses []string) (Application, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Application{}, fmt.Errorf("begin add application: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"INSERT INTO applications (domain_id, login, address_mode, created_at) VALUES (?, ?, ?, ?)",
		domainID, login, mode, now.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Application{}, ErrLoginExists
		}
		return Application{}, fmt.Errorf("insert application: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Application{}, fmt.Errorf("application id: %w", err)
	}
	if err := insertAddresses(tx, id, mode, addresses); err != nil {
		return Application{}, err
	}
	if err := tx.Commit(); err != nil {
		return Application{}, fmt.Errorf("commit add application: %w", err)
	}
	return Application{
		ID: id, DomainID: domainID, Login: login, AddressMode: mode,
		CreatedAt: now, Addresses: normalizedList(mode, addresses),
	}, nil
}

// UpdateApplicationMode switches an application's address mode and replaces its
// address list atomically (spec 7.2.7). The login and password are untouched.
// Returns ErrApplicationNotFound if the id does not exist.
func (s *Store) UpdateApplicationMode(id int64, mode string, addresses []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin update application: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE applications SET address_mode = ? WHERE id = ?", mode, id)
	if err != nil {
		return fmt.Errorf("update application mode: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update application rows: %w", err)
	}
	if n == 0 {
		return ErrApplicationNotFound
	}
	if _, err := tx.Exec("DELETE FROM application_addresses WHERE application_id = ?", id); err != nil {
		return fmt.Errorf("clear addresses: %w", err)
	}
	if err := insertAddresses(tx, id, mode, addresses); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update application: %w", err)
	}
	return nil
}

// insertAddresses writes the address rows for a list-mode application. In
// wildcard mode no address rows are stored (the wildcard is derived from the
// domain at map-generation time).
func insertAddresses(tx *sql.Tx, appID int64, mode string, addresses []string) error {
	if mode != AddressModeList {
		return nil
	}
	for _, addr := range addresses {
		if _, err := tx.Exec(
			"INSERT INTO application_addresses (application_id, address) VALUES (?, ?)",
			appID, addr,
		); err != nil {
			if isUniqueViolation(err) {
				continue // a repeated address in the same submission is harmless
			}
			return fmt.Errorf("insert address %q: %w", addr, err)
		}
	}
	return nil
}

func normalizedList(mode string, addresses []string) []string {
	if mode != AddressModeList {
		return nil
	}
	return addresses
}

// GetApplication returns one application (with its addresses) by id, or
// ErrApplicationNotFound.
func (s *Store) GetApplication(id int64) (Application, error) {
	row := s.db.QueryRow(
		"SELECT id, domain_id, login, address_mode, created_at FROM applications WHERE id = ?", id)
	a, err := scanApplication(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrApplicationNotFound
	}
	if err != nil {
		return Application{}, err
	}
	addrs, err := s.applicationAddresses(a.ID)
	if err != nil {
		return Application{}, err
	}
	a.Addresses = addrs
	return a, nil
}

// ListApplicationsByDomain returns a domain's applications ordered by login,
// each with its address list populated (spec 7.2.6).
func (s *Store) ListApplicationsByDomain(domainID int64) ([]Application, error) {
	rows, err := s.db.Query(
		"SELECT id, domain_id, login, address_mode, created_at FROM applications WHERE domain_id = ? ORDER BY login",
		domainID)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()

	var out []Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Fill address lists after the first query is drained (MaxOpenConns is 1).
	for i := range out {
		addrs, err := s.applicationAddresses(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Addresses = addrs
	}
	return out, nil
}

// ListLoginsByDomain returns the SASL logins of a domain's applications. Used to
// purge sasldb2 entries before a domain (and its applications via cascade) is
// deleted, while the logins are still known (spec 7.2.4).
func (s *Store) ListLoginsByDomain(domainID int64) ([]string, error) {
	rows, err := s.db.Query("SELECT login FROM applications WHERE domain_id = ? ORDER BY login", domainID)
	if err != nil {
		return nil, fmt.Errorf("list logins: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, err
		}
		out = append(out, login)
	}
	return out, rows.Err()
}

// ListBindings returns every sender-address → login pair across all domains, the
// raw material for the smtpd_sender_login_maps file (spec 5.1). Wildcard
// applications yield a single "@domain" binding; list applications yield one
// binding per address. Ordered deterministically so the generated map is stable.
func (s *Store) ListBindings() ([]Binding, error) {
	rows, err := s.db.Query(`
		SELECT '@' || d.name, a.login
		FROM applications a
		JOIN domains d ON d.id = a.domain_id
		WHERE a.address_mode = 'wildcard'
		UNION ALL
		SELECT aa.address, a.login
		FROM application_addresses aa
		JOIN applications a ON a.id = aa.application_id
		WHERE a.address_mode = 'list'
		ORDER BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	defer rows.Close()

	var out []Binding
	for rows.Next() {
		var b Binding
		if err := rows.Scan(&b.Address, &b.Login); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteApplication removes an application and its addresses (via cascade),
// returning the deleted application so the caller can drop its sasldb2 entry
// (spec 7.2.8). Returns ErrApplicationNotFound if no such row existed.
func (s *Store) DeleteApplication(id int64) (Application, error) {
	a, err := s.GetApplication(id)
	if err != nil {
		return Application{}, err
	}
	res, err := s.db.Exec("DELETE FROM applications WHERE id = ?", id)
	if err != nil {
		return Application{}, fmt.Errorf("delete application: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Application{}, fmt.Errorf("delete application rows: %w", err)
	}
	if n == 0 {
		return Application{}, ErrApplicationNotFound
	}
	return a, nil
}

func (s *Store) applicationAddresses(appID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT address FROM application_addresses WHERE application_id = ? ORDER BY address", appID)
	if err != nil {
		return nil, fmt.Errorf("application addresses: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanApplication(r scanRow) (Application, error) {
	var (
		a         Application
		createdAt string
	)
	if err := r.Scan(&a.ID, &a.DomainID, &a.Login, &a.AddressMode, &createdAt); err != nil {
		return Application{}, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return a, nil
}
