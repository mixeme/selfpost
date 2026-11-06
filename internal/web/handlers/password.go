package handlers

import "github.com/mixeme/selfpost/internal/web/auth"

// bcryptCost reads the panel-wide password work factor; see auth.BcryptCost
// for why it lives in one place. A function keeps tests' lowered cost visible.
func bcryptCost() int { return auth.BcryptCost }
