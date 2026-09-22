# SelfPost build.
#
# The version is stamped into both binaries and MUST match the Docker image tag
# (spec 7.5.A: backup/restore compatibility check). Override on the command line:
#
#     make build VERSION=1.3.0
#
# CGO is disabled to keep the binaries fully static (modernc.org/sqlite is pure
# Go, so no cgo is required) — see spec 7.1.

VERSION ?= dev
MODULE  := github.com/mixeme/selfpost
LDFLAGS := -X $(MODULE)/internal/buildinfo.Version=$(VERSION)
GOFLAGS := -trimpath

.PHONY: all build vet test clean e2e

all: vet build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/panel ./cmd/panel
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/selfpost-backup ./cmd/selfpost-backup

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin

# Hermetic container e2e (see docs/development.md): separate Go module
# under test/e2e so its test-only dependencies (DKIM verification) never enter
# this module's build graph. Builds the image fresh from this checkout, brings
# up deploy/docker-compose.yml plus a test-only override on high ports and an
# isolated compose project (-p selfpost-e2e) so it never collides with a real
# deployment on the same host, then tears the stand down whether the suite
# passed or not.
e2e:
	cd test/e2e && go test -v -timeout 20m ./...
