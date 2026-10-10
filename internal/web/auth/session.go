package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// renewThreshold bounds how often an active session's expiry is written back
// to the database. Renewing on every request would mean a write (and a new
// Set-Cookie) per click; renewing at most once an hour keeps that cost low
// while still keeping a busy admin's session alive indefinitely (plan B.1).
const renewThreshold = time.Hour

// sessionStore persists login sessions in the database (plan B.1): a login
// survives a container restart or redeploy. Only the SHA-256 of the token is
// stored, never the token itself (security.md's crypto-random bearer token), so
// a stolen database file or backup archive cannot be replayed as a session —
// it only extends the login of whichever browser still holds the original
// cookie.
type sessionStore struct {
	store *store.Store
	idle  time.Duration
}

func newSessionStore(st *store.Store, idle time.Duration) *sessionStore {
	return &sessionStore{store: st, idle: idle}
}

// MaxAge is the session cookie's Max-Age in seconds, kept equal to the
// sliding idle window so the browser drops the cookie no later than the
// server would have expired it anyway.
func (s *sessionStore) MaxAge() int {
	return int(s.idle.Seconds())
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create issues a new session for the user and returns its token. It fails
// closed: if the row cannot be written the caller gets an error and must not
// hand out a cookie, because a token that is not in the database looks like a
// signed-in browser while every request it makes bounces back to /login.
func (s *sessionStore) Create(userID int64) (string, error) {
	token := randomToken(32)
	now := time.Now()
	if err := s.store.CreateSession(hashToken(token), userID, now.Add(s.idle)); err != nil {
		return "", err
	}
	// Pruning is housekeeping: the new session is already valid, so a failure
	// here is logged and does not fail the login.
	if _, err := s.store.DeleteExpiredSessions(now); err != nil {
		logf("panel: session: prune expired failed: %v", err)
	}
	return token, nil
}

// Lookup returns the id of the user a token's session belongs to, if the
// session exists and is unexpired.
func (s *sessionStore) Lookup(token string) (int64, bool) {
	if token == "" {
		return 0, false
	}
	hash := hashToken(token)
	row, found, err := s.store.LookupSession(hash)
	if err != nil {
		logf("panel: session: lookup failed: %v", err)
		return 0, false
	}
	if !found {
		return 0, false
	}
	if time.Now().After(row.ExpiresAt) {
		if err := s.store.DeleteSession(hash); err != nil {
			logf("panel: session: delete expired failed: %v", err)
		}
		return 0, false
	}
	return row.UserID, true
}

// Touch extends a session's sliding expiry if it has been at least
// renewThreshold since the last extension, and reports whether it did so.
func (s *sessionStore) Touch(token string) bool {
	hash := hashToken(token)
	row, found, err := s.store.LookupSession(hash)
	if err != nil {
		logf("panel: session: touch lookup failed: %v", err)
		return false
	}
	if !found {
		return false
	}
	lastRenewal := row.ExpiresAt.Add(-s.idle)
	now := time.Now()
	if now.Sub(lastRenewal) < renewThreshold {
		return false
	}
	if err := s.store.RenewSession(hash, now.Add(s.idle)); err != nil {
		logf("panel: session: renew failed: %v", err)
		return false
	}
	return true
}

// DestroyOthers invalidates every session of the user except keep.
func (s *sessionStore) DestroyOthers(userID int64, keep string) {
	if err := s.store.DeleteOtherSessions(userID, hashToken(keep)); err != nil {
		logf("panel: session: destroy others failed: %v", err)
	}
}

// DestroyAll invalidates every session of the user.
func (s *sessionStore) DestroyAll(userID int64) {
	if err := s.store.DeleteUserSessions(userID); err != nil {
		logf("panel: session: destroy all failed: %v", err)
	}
}

// Destroy invalidates a session token (logout).
func (s *sessionStore) Destroy(token string) {
	if err := s.store.DeleteSession(hashToken(token)); err != nil {
		logf("panel: session: destroy failed: %v", err)
	}
}
