package app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// passwordBytes is the number of random bytes behind a generated application
// password. 24 bytes = 192 bits of entropy, well above any brute-force concern
// for a SASL credential the panel shows exactly once (spec 7.6.1).
const passwordBytes = 24

// generatePassword returns a strong, URL-safe random password for an
// application's SASL account. The panel generates it, shows it once and never
// stores the plaintext (spec 7.6.1); sasldb2 keeps only the hashed form.
//
// base64url output keeps the password to a safe ASCII alphabet with no shell or
// SMTP-special characters, so it survives being typed into client configuration
// and passed to saslpasswd2 over stdin unchanged.
func generatePassword() (string, error) {
	buf := make([]byte, passwordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
