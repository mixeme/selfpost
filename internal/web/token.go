package web

import (
	"crypto/rand"
	"encoding/base64"
)

// randomToken returns a URL-safe token with at least nBytes*8 bits of entropy
// drawn from crypto/rand. Setup and session tokens both use this; the setup
// token needs >=128 bits (spec 7.6.1), so callers pass nBytes >= 16.
//
// It panics if the system RNG fails: that is unrecoverable and must never be
// papered over with a weak fallback for a security token.
func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
