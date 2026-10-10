package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SessionRow is a persisted login session, keyed by the SHA-256 of its token
// (see internal/web, which owns the token itself). It belongs to a user by id:
// deleting the user deletes their sessions, and renaming them keeps them.
type SessionRow struct {
	UserID    int64
	ExpiresAt time.Time
}

// CreateSession inserts a new session row for a user.
func (s *Store) CreateSession(tokenHash string, userID int64, expiresAt time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, now, expiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// LookupSession returns the session for tokenHash, if any. It does not check
// expiry itself — callers compare ExpiresAt against time.Now() and call
// DeleteSession on an expired row, keeping the read side lock-free.
func (s *Store) LookupSession(tokenHash string) (SessionRow, bool, error) {
	var (
		row       SessionRow
		expiresAt string
	)
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`,
		tokenHash,
	).Scan(&row.UserID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRow{}, false, nil
	}
	if err != nil {
		return SessionRow{}, false, fmt.Errorf("lookup session: %w", err)
	}
	row.ExpiresAt, err = time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return SessionRow{}, false, fmt.Errorf("parse session expiry: %w", err)
	}
	return row, true, nil
}

// RenewSession pushes a session's expiry forward, implementing the sliding
// idle timeout.
func (s *Store) RenewSession(tokenHash string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET expires_at = ? WHERE token_hash = ?`,
		expiresAt.UTC().Format(time.RFC3339), tokenHash,
	)
	if err != nil {
		return fmt.Errorf("renew session: %w", err)
	}
	return nil
}

// DeleteSession removes a session row (logout, or a lookup finding it
// expired).
func (s *Store) DeleteSession(tokenHash string) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteOtherSessions removes every session of the user except keepHash. It is
// called when the user changes their password: a stolen cookie issued under the
// old password must stop working, while the session performing the change
// stays signed in. Other users' sessions are not touched.
func (s *Store) DeleteOtherSessions(userID int64, keepHash string) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, keepHash); err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	return nil
}

// DeleteUserSessions removes every session of the user, for when somebody else
// has set their password.
func (s *Store) DeleteUserSessions(userID int64) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// DeleteExpiredSessions prunes rows whose expiry has already passed, so an
// abandoned session (cookie never presented again) does not sit in the table
// forever. It piggybacks on session creation rather than running as its own
// background loop.
func (s *Store) DeleteExpiredSessions(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("prune sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
