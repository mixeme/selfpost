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
MODULE  := codeberg.org/mix/selfpost
LDFLAGS := -X $(MODULE)/internal/buildinfo.Version=$(VERSION)
GOFLAGS := -trimpath

.PHONY: all build vet test clean

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
