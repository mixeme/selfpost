package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The purposes of a user link (migration 0013).
const (
	LinkConfirm = "confirm" // confirms the account's e-mail
	LinkReset   = "reset"   // sets a new password
)

// ErrNoLink is a link that does not exist, has been taken or has expired:
// the three are one answer.
var ErrNoLink = errors.New("no such link")

// UserLink is a one-time link of a user, as stored: the token itself is in
// the mail alone.
type UserLink struct {
	UserID    int64
	Purpose   string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateUserLink stores a link of the user for purpose, sent to email, valid
// until expires, and removes every expired link. A confirmation link replaces
// the user's earlier one - it confirms the address saved last. A reset link
// does not: anyone who knows a username can ask for one, and a request of
// theirs must not kill the link the user is about to open (security review,
// 2026-10-17); setting a password removes them all.
func (s *Store) CreateUserLink(tokenHash string, userID int64, purpose, email string, now, expires time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("create user link: %w", err)
	}
	defer tx.Rollback()
	// The first statement is a write (see UpdateStatus).
	if _, err := tx.Exec(`DELETE FROM user_links WHERE (user_id = ? AND purpose = ? AND purpose = ?) OR expires_at <= ?`,
		userID, purpose, LinkConfirm, now.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("create user link: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO user_links (token_hash, user_id, purpose, email, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, tokenHash, userID, purpose, email,
		now.UTC().Format(time.RFC3339), expires.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("create user link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create user link: %w", err)
	}
	return nil
}

func scanLink(r scanRow) (UserLink, error) {
	var l UserLink
	var created, expires string
	if err := r.Scan(&l.UserID, &l.Purpose, &l.Email, &created, &expires); err != nil {
		return UserLink{}, err
	}
	l.CreatedAt, _ = time.Parse(time.RFC3339, created)
	l.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	return l, nil
}

const linkColumns = "user_id, purpose, email, created_at, expires_at"

// PeekUserLink returns the link of purpose with tokenHash if it is still
// valid at now, without taking it: the page a link opens shows its form.
func (s *Store) PeekUserLink(tokenHash, purpose string, now time.Time) (UserLink, error) {
	l, err := scanLink(s.db.QueryRow(`SELECT `+linkColumns+` FROM user_links
		WHERE token_hash = ? AND purpose = ? AND expires_at > ?`,
		tokenHash, purpose, now.UTC().Format(time.RFC3339)))
	if errors.Is(err, sql.ErrNoRows) {
		return UserLink{}, ErrNoLink
	}
	if err != nil {
		return UserLink{}, fmt.Errorf("peek user link: %w", err)
	}
	return l, nil
}

// TakeUserLink returns the link of purpose with tokenHash and deletes it, in
// one transaction: of two requests with the same link one gets it.
func (s *Store) TakeUserLink(tokenHash, purpose string, now time.Time) (UserLink, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return UserLink{}, fmt.Errorf("take user link: %w", err)
	}
	defer tx.Rollback()
	// The delete first: it is the write that takes the lock (see
	// UpdateStatus), and RETURNING gives the row it removed (SQLite 3.35+,
	// https://sqlite.org/lang_returning.html).
	l, err := scanLink(tx.QueryRow(`DELETE FROM user_links
		WHERE token_hash = ? AND purpose = ? AND expires_at > ?
		RETURNING `+linkColumns, tokenHash, purpose, now.UTC().Format(time.RFC3339)))
	if errors.Is(err, sql.ErrNoRows) {
		return UserLink{}, ErrNoLink
	}
	if err != nil {
		return UserLink{}, fmt.Errorf("take user link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return UserLink{}, fmt.Errorf("take user link: %w", err)
	}
	return l, nil
}

// LatestUserLink is the user's live link of purpose, ErrNoLink when there is
// none: Account says when the confirmation link went.
func (s *Store) LatestUserLink(userID int64, purpose string, now time.Time) (UserLink, error) {
	l, err := scanLink(s.db.QueryRow(`SELECT `+linkColumns+` FROM user_links
		WHERE user_id = ? AND purpose = ? AND expires_at > ? ORDER BY created_at DESC LIMIT 1`,
		userID, purpose, now.UTC().Format(time.RFC3339)))
	if errors.Is(err, sql.ErrNoRows) {
		return UserLink{}, ErrNoLink
	}
	if err != nil {
		return UserLink{}, fmt.Errorf("latest user link: %w", err)
	}
	return l, nil
}

// DeleteUserLinks removes every link of the user of purpose: a password set
// another way leaves no reset link behind it.
func (s *Store) DeleteUserLinks(userID int64, purpose string) error {
	if _, err := s.db.Exec(`DELETE FROM user_links WHERE user_id = ? AND purpose = ?`, userID, purpose); err != nil {
		return fmt.Errorf("delete user links: %w", err)
	}
	return nil
}

// ConfirmEmail marks the user's e-mail confirmed at now if it is still email:
// false when the address has changed since the link was sent.
func (s *Store) ConfirmEmail(userID int64, email string, now time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE users SET email_confirmed_at = ? WHERE id = ? AND email = ? AND email <> ''`,
		now.UTC().Format(time.RFC3339), userID, email)
	if err != nil {
		return false, fmt.Errorf("confirm e-mail: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// TakeResetLink sets the password of the reset link with tokenHash, in one
// transaction: the link is taken, the password set only while the account's
// address is still the confirmed one the link was sent to, and the user's
// other reset links go. ErrNoLink, and nothing changed, for a link that does
// not hold - taken, expired, or of an address the account no longer has.
func (s *Store) TakeResetLink(tokenHash, passwordHash string, now time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("take reset link: %w", err)
	}
	defer tx.Rollback()
	l, err := scanLink(tx.QueryRow(`DELETE FROM user_links
		WHERE token_hash = ? AND purpose = ? AND expires_at > ?
		RETURNING `+linkColumns, tokenHash, LinkReset, now.UTC().Format(time.RFC3339)))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoLink
	}
	if err != nil {
		return 0, fmt.Errorf("take reset link: %w", err)
	}
	res, err := tx.Exec(`UPDATE users SET password_hash = ?
		WHERE id = ? AND email = ? AND email <> '' AND email_confirmed_at <> ''`, passwordHash, l.UserID, l.Email)
	if err != nil {
		return 0, fmt.Errorf("take reset link: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, ErrNoLink
	}
	if _, err := tx.Exec(`DELETE FROM user_links WHERE user_id = ? AND purpose = ?`, l.UserID, LinkReset); err != nil {
		return 0, fmt.Errorf("take reset link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("take reset link: %w", err)
	}
	return l.UserID, nil
}

// DeleteAllUserLinks removes every link: a restored backup must not bring
// back one that was taken after it was made (security review, 2026-10-17).
func (s *Store) DeleteAllUserLinks() error {
	if _, err := s.db.Exec(`DELETE FROM user_links`); err != nil {
		return fmt.Errorf("delete user links: %w", err)
	}
	return nil
}

// SetUserPassword replaces the user's password hash alone.
func (s *Store) SetUserPassword(userID int64, passwordHash string) error {
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}
