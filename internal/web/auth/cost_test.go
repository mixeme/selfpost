package auth

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain keeps the suite at bcrypt's cheapest valid work factor. The one
// timing test that checks the real cost sets BcryptCost back to 12 itself.
func TestMain(m *testing.M) {
	BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
