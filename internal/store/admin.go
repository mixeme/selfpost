package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoAdmin is returned by GetAdmin when primary setup has not happened yet.
var ErrNoAdmin = errors.New("no administrator account")

// Admin is the single panel administrator (spec 7.6.1).
type Admin struct {
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// AdminExists reports whether the administrator account has been created. This
// doubles as the "primary setup complete" flag: once true, the /setup route is
// permanently gone (spec 7.6.1).
func (s *Store) AdminExists() (bool, error) {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM admin").Scan(&n); err != nil {
		return false, fmt.Errorf("count admin: %w", err)
	}
	return n > 0, nil
}

// CreateAdmin inserts the administrator row. It fails if one already exists,
// which — combined with the id=1 constraint — makes admin creation one-shot
// even under a race between two setup submissions.
func (s *Store) CreateAdmin(username, passwordHash string) error {
	_, err := s.db.Exec(
		"INSERT INTO admin (id, username, password_hash, created_at) VALUES (1, ?, ?, ?)",
		username, passwordHash, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	return nil
}

// GetAdmin returns the administrator account, or ErrNoAdmin if setup is pending.
func (s *Store) GetAdmin() (Admin, error) {
	var (
		a         Admin
		createdAt string
	)
	err := s.db.QueryRow("SELECT username, password_hash, created_at FROM admin WHERE id = 1").
		Scan(&a.Username, &a.PasswordHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrNoAdmin
	}
	if err != nil {
		return Admin{}, fmt.Errorf("get admin: %w", err)
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return a, nil
}
