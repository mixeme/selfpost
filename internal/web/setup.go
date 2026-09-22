package web

import (
	"crypto/subtle"
	"fmt"
	"os"
	"sync"
	"time"

	"codeberg.org/mix/selfpost/internal/store"
)

// setupTokenTTL is the lifetime of a setup token (spec 7.6.1). After it
// elapses the token is regenerated and re-announced on the next /setup hit.
const setupTokenTTL = 10 * time.Minute

// setupManager owns the one-time administrator setup token. The token itself is
// ephemeral (regenerated on restart or expiry) and lives only in memory; the
// persistent "setup complete" fact is the presence of the admin row in the
// store, so once that exists the token is gone for good (spec 7.6.1).
type setupManager struct {
	store     *store.Store
	hostname  string
	tokenPath string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newSetupManager(st *store.Store, hostname, tokenPath string) *setupManager {
	return &setupManager{store: st, hostname: hostname, tokenPath: tokenPath}
}

// bootstrap runs once at startup. If setup is already complete it clears any
// stale token file; otherwise it mints and announces the first token.
func (m *setupManager) bootstrap() error {
	done, err := m.store.AdminExists()
	if err != nil {
		return err
	}
	if done {
		m.clearTokenFile()
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.regenerateLocked()
	return nil
}

// activeToken returns the current valid setup token, regenerating and
// re-announcing it if none exists or it has expired. It returns ("", false)
// once setup is complete — callers must treat that as "route gone" (404).
func (m *setupManager) activeToken() (string, bool) {
	done, err := m.store.AdminExists()
	if err != nil {
		logf("panel: setup: admin check failed: %v", err)
		return "", false
	}
	if done {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token == "" || time.Now().After(m.expiresAt) {
		m.regenerateLocked()
	}
	return m.token, true
}

// validate reports whether provided matches the active token, using a
// constant-time comparison to avoid leaking a correct prefix via timing
// (spec 7.6.1). A mismatch does NOT regenerate or invalidate the token: failed
// attempts must not let an attacker DoS a legitimate setup (spec 7.6.1).
func (m *setupManager) validate(provided string) bool {
	token, ok := m.activeToken()
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

// complete marks setup as finished: the admin row now exists, so drop the
// in-memory token and remove the on-disk copy.
func (m *setupManager) complete() {
	m.mu.Lock()
	m.token = ""
	m.expiresAt = time.Time{}
	m.mu.Unlock()
	m.clearTokenFile()
}

// regenerateLocked mints a fresh token, announces it and mirrors it to disk.
// Caller holds m.mu.
func (m *setupManager) regenerateLocked() {
	m.token = randomToken(16) // 128 bits of entropy (spec 7.6.1)
	m.expiresAt = time.Now().Add(setupTokenTTL)
	m.announce(m.token)
}

// announce prints the setup link to the container log and writes it to the
// token file so it can be read either way (spec 7.6.1).
func (m *setupManager) announce(token string) {
	url := m.setupURL(token)
	logf("panel: ==================================================================")
	logf("panel: SelfPost first-run setup — open this one-time link within %s:", setupTokenTTL)
	logf("panel:   %s", url)
	logf("panel: (also written to %s)", m.tokenPath)
	logf("panel: ==================================================================")

	if m.tokenPath == "" {
		return
	}
	// 0600: the token is a bearer secret for creating the admin.
	if err := os.WriteFile(m.tokenPath, []byte(url+"\n"), 0o600); err != nil {
		logf("panel: setup: could not write token file %s: %v", m.tokenPath, err)
	}
}

func (m *setupManager) setupURL(token string) string {
	host := m.hostname
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("https://%s/setup/%s", host, token)
}

func (m *setupManager) clearTokenFile() {
	if m.tokenPath == "" {
		return
	}
	if err := os.Remove(m.tokenPath); err != nil && !os.IsNotExist(err) {
		logf("panel: setup: could not remove token file %s: %v", m.tokenPath, err)
	}
}
