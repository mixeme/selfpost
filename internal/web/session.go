package web

import (
	"sync"
	"time"
)

// sessionTTL bounds how long a login lasts before re-authentication is needed.
const sessionTTL = 12 * time.Hour

// sessionStore keeps active sessions in memory. Sessions are deliberately not
// persisted (spec 9 lists what must survive restart; sessions are not on it):
// a restart simply logs the admin out, which is acceptable and avoids storing
// bearer tokens on disk. Tokens are crypto-random (spec 7.6.6).
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

type session struct {
	username  string
	expiresAt time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]session)}
}

// Create issues a new session for username and returns its token.
func (s *sessionStore) Create(username string) string {
	token := randomToken(32)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session{username: username, expiresAt: time.Now().Add(sessionTTL)}
	return token
}

// Lookup returns the session username for a token if it exists and is unexpired.
func (s *sessionStore) Lookup(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return "", false
	}
	if now.After(sess.expiresAt) {
		delete(s.sessions, token)
		return "", false
	}
	return sess.username, true
}

// Rename updates the username carried by a session, keeping its expiry. It is
// used when the administrator renames their own account so the current session
// keeps working under the new name.
func (s *sessionStore) Rename(token, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[token]; ok {
		sess.username = username
		s.sessions[token] = sess
	}
}

// DestroyOthers invalidates every session except keep. It is called when the
// administrator changes their password: a stolen cookie issued under the old
// password must stop working, while the admin performing the change stays
// signed in.
func (s *sessionStore) DestroyOthers(keep string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token := range s.sessions {
		if token != keep {
			delete(s.sessions, token)
		}
	}
}

// Destroy invalidates a session token (logout).
func (s *sessionStore) Destroy(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}
