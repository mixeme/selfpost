// Package buildinfo exposes build-time metadata stamped via -ldflags.
package buildinfo

// Version is the SelfPost release. It is set at build time with
//
//	-ldflags "-X github.com/mixeme/selfpost/internal/buildinfo.Version=<tag>"
//
// and must match the Docker image tag; it is used for the backup/restore
// compatibility check (architecture.md § Persistence). Defaults to "dev" for local/unstamped builds.
var Version = "dev"
