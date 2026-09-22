package web

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
	// idle is the sliding inactivity window (PANEL_SESSION_IDLE_DAYS). There is
	// no absolute cap: an administrator who keeps coming back stays signed in
	// indefinitely, deliberately.
	idle time.Duration
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

// Create issues a new session for username and returns its token.
func (s *sessionStore) Create(username string) string {
	token := randomToken(32)
	now := time.Now()
	if err := s.store.CreateSession(hashToken(token), username, now.Add(s.idle)); err != nil {
		logf("panel: session: create failed: %v", err)
	}
	// Opportunistic cleanup: a session nobody ever came back to otherwise sits
	// in the table forever. Piggybacking on Create (the one write every login
	// already pays for) avoids a dedicated background sweep for what is, on a
	// single-admin panel, a handful of rows at most.
	if _, err := s.store.DeleteExpiredSessions(now); err != nil {
		logf("panel: session: prune expired failed: %v", err)
	}
	return token
}

// Lookup returns the session username for a token if it exists and is
// unexpired.
func (s *sessionStore) Lookup(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	hash := hashToken(token)
	row, found, err := s.store.LookupSession(hash)
	if err != nil {
		logf("panel: session: lookup failed: %v", err)
		return "", false
	}
	if !found {
		return "", false
	}
	if time.Now().After(row.ExpiresAt) {
		if err := s.store.DeleteSession(hash); err != nil {
			logf("panel: session: delete expired failed: %v", err)
		}
		return "", false
	}
	return row.Username, true
}

// Touch extends a session's sliding expiry if it has been at least
// renewThreshold since the last extension, and reports whether it did so —
// the caller uses that to decide whether the response needs a fresh
// Set-Cookie. It assumes the caller has just confirmed the session is valid
// (e.g. via Lookup); it does nothing for a token that no longer exists.
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
	// expiresAt = lastRenewal + idle, so this recovers when the session was
	// last extended without a separate column.
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

// Rename updates the username carried by a session, keeping its expiry. It is
// used when the administrator renames their own account so the current
// session keeps working under the new name.
func (s *sessionStore) Rename(token, username string) {
	if err := s.store.RenameSession(hashToken(token), username); err != nil {
		logf("panel: session: rename failed: %v", err)
	}
}

// DestroyOthers invalidates every session except keep. It is called when the
// administrator changes their password: a stolen cookie issued under the old
// password must stop working, while the admin performing the change stays
// signed in.
func (s *sessionStore) DestroyOthers(keep string) {
	if err := s.store.DeleteOtherSessions(hashToken(keep)); err != nil {
		logf("panel: session: destroy others failed: %v", err)
	}
}

// Destroy invalidates a session token (logout).
func (s *sessionStore) Destroy(token string) {
	if err := s.store.DeleteSession(hashToken(token)); err != nil {
		logf("panel: session: destroy failed: %v", err)
	}
}
