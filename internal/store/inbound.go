package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Recipient modes for an inbound domain. Kept in sync with the CHECK constraint.
const (
	RecipientModeList = "list" // only explicitly listed addresses
	RecipientModeAny  = "any"  // any address at the domain
)

// TLS modes for the hand-off to the upstream. Values are Postfix
// smtp_tls_policy_maps levels: may (opportunistic), encrypt (required), none.
const (
	TLSModeMay     = "may"
	TLSModeEncrypt = "encrypt"
	TLSModeNone    = "none"
)

// ErrInboundDomainExists is returned when the inbound domain is already configured.
var ErrInboundDomainExists = errors.New("inbound domain already exists")

// ErrInboundDomainNotFound is returned when an inbound domain id/name does not exist.
var ErrInboundDomainNotFound = errors.New("inbound domain not found")

// InboundDomain is a backup-MX / forwarder domain. Host may be empty until the
// operator saves an upstream; map generation skips those rows so mail is never
// accepted with nowhere to send it. RecipientCount is populated by listing
// queries; Recipients is populated by Get.
type InboundDomain struct {
	ID             int64
	Name           string
	RecipientMode  string
	Host           string
	Port           int
	TLSMode        string
	CreatedAt      time.Time
	RecipientCount int
	Recipients     []string
}

// AddInboundDomain inserts a new inbound domain with a default transport
// (empty host, port 25, opportunistic TLS) and listed-recipients mode. The
// caller must have validated name (security.md).
func (s *Store) AddInboundDomain(name string) (InboundDomain, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return InboundDomain{}, fmt.Errorf("begin add inbound domain: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"INSERT INTO inbound_domains (name, recipient_mode, created_at) VALUES (?, ?, ?)",
		name, RecipientModeList, now.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return InboundDomain{}, ErrInboundDomainExists
		}
		return InboundDomain{}, fmt.Errorf("insert inbound domain: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return InboundDomain{}, fmt.Errorf("inbound domain id: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO inbound_transports (inbound_domain_id, host, port, tls_mode) VALUES (?, '', 25, ?)",
		id, TLSModeMay,
	); err != nil {
		return InboundDomain{}, fmt.Errorf("insert inbound transport: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return InboundDomain{}, fmt.Errorf("commit add inbound domain: %w", err)
	}
	return InboundDomain{
		ID:            id,
		Name:          name,
		RecipientMode: RecipientModeList,
		Port:          25,
		TLSMode:       TLSModeMay,
		CreatedAt:     now,
	}, nil
}

// ListInboundDomains returns every inbound domain with its transport and
// recipient count, ordered by name.
func (s *Store) ListInboundDomains() ([]InboundDomain, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.name, d.recipient_mode, d.created_at,
		       t.host, t.port, t.tls_mode,
		       (SELECT COUNT(*) FROM inbound_recipients r WHERE r.inbound_domain_id = d.id)
		FROM inbound_domains d
		INNER JOIN inbound_transports t ON t.inbound_domain_id = d.id
		ORDER BY d.name`)
	if err != nil {
		return nil, fmt.Errorf("list inbound domains: %w", err)
	}
	defer rows.Close()

	var out []InboundDomain
	for rows.Next() {
		d, err := scanInboundDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetInboundDomain returns one inbound domain with its recipient list, or
// ErrInboundDomainNotFound.
func (s *Store) GetInboundDomain(id int64) (InboundDomain, error) {
	row := s.db.QueryRow(`
		SELECT d.id, d.name, d.recipient_mode, d.created_at,
		       t.host, t.port, t.tls_mode,
		       (SELECT COUNT(*) FROM inbound_recipients r WHERE r.inbound_domain_id = d.id)
		FROM inbound_domains d
		INNER JOIN inbound_transports t ON t.inbound_domain_id = d.id
		WHERE d.id = ?`, id)
	d, err := scanInboundDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return InboundDomain{}, ErrInboundDomainNotFound
	}
	if err != nil {
		return InboundDomain{}, err
	}
	addrs, err := s.listInboundRecipients(id)
	if err != nil {
		return InboundDomain{}, err
	}
	d.Recipients = addrs
	return d, nil
}

// UpdateInboundTransport sets the upstream host, port and TLS mode.
func (s *Store) UpdateInboundTransport(id int64, host string, port int, tlsMode string) error {
	res, err := s.db.Exec(
		"UPDATE inbound_transports SET host = ?, port = ?, tls_mode = ? WHERE inbound_domain_id = ?",
		host, port, tlsMode, id,
	)
	if err != nil {
		return fmt.Errorf("update inbound transport: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update inbound transport rows: %w", err)
	}
	if n == 0 {
		return ErrInboundDomainNotFound
	}
	return nil
}

// UpdateInboundRecipients replaces the recipient mode and, in list mode, the
// address list. In any mode the stored list is cleared.
func (s *Store) UpdateInboundRecipients(id int64, mode string, addresses []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin update inbound recipients: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE inbound_domains SET recipient_mode = ? WHERE id = ?", mode, id)
	if err != nil {
		return fmt.Errorf("update inbound recipient mode: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update inbound recipient mode rows: %w", err)
	}
	if n == 0 {
		return ErrInboundDomainNotFound
	}
	if _, err := tx.Exec("DELETE FROM inbound_recipients WHERE inbound_domain_id = ?", id); err != nil {
		return fmt.Errorf("clear inbound recipients: %w", err)
	}
	if mode == RecipientModeList {
		for _, addr := range addresses {
			if _, err := tx.Exec(
				"INSERT INTO inbound_recipients (inbound_domain_id, address) VALUES (?, ?)",
				id, addr,
			); err != nil {
				return fmt.Errorf("insert inbound recipient: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update inbound recipients: %w", err)
	}
	return nil
}

// DeleteInboundDomain removes an inbound domain and its transport/recipients
// (ON DELETE CASCADE). Returns ErrInboundDomainNotFound if no such row existed.
func (s *Store) DeleteInboundDomain(id int64) error {
	res, err := s.db.Exec("DELETE FROM inbound_domains WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete inbound domain: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete inbound domain rows: %w", err)
	}
	if n == 0 {
		return ErrInboundDomainNotFound
	}
	return nil
}

func (s *Store) listInboundRecipients(id int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT address FROM inbound_recipients WHERE inbound_domain_id = ? ORDER BY address",
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list inbound recipients: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		out = append(out, addr)
	}
	return out, rows.Err()
}

func scanInboundDomain(r scanRow) (InboundDomain, error) {
	var (
		d         InboundDomain
		createdAt string
	)
	if err := r.Scan(
		&d.ID, &d.Name, &d.RecipientMode, &createdAt,
		&d.Host, &d.Port, &d.TLSMode, &d.RecipientCount,
	); err != nil {
		return InboundDomain{}, err
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return d, nil
}
