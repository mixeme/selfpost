package handlers

import "github.com/mixeme/selfpost/internal/web/auth"

// bcryptCost is the panel-wide password work factor; see auth.BcryptCost for
// why it lives in one place.
const bcryptCost = auth.BcryptCost
