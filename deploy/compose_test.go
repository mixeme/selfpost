package deploy

import (
	"bytes"
	"os"
	"regexp"
	"testing"
)

// The compose file names the version twice: in the image tag, and in the
// VERSION its build: section stamps an image with when it builds one. A
// release that bumps one and not the other would build an image whose
// version is not its tag, and the backup check compares the two.
func TestComposeBuildVersionIsTheImageTag(t *testing.T) {
	b, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	image := regexp.MustCompile(`(?m)^\s+image: ghcr\.io/mixeme/selfpost:(\S+)$`).FindSubmatch(b)
	version := regexp.MustCompile(`(?m)^\s+VERSION: (\S+)$`).FindSubmatch(b)
	if image == nil || version == nil {
		t.Fatalf("image tag %q, build VERSION %q: one is missing", image, version)
	}
	if string(image[1]) != string(version[1]) {
		t.Errorf("image tag %s, build VERSION %s: they must be one version", image[1], version[1])
	}
}

// The build context's default is a directory that cannot exist by accident
// (review 2026-11-05, FT2). The guide's installation step 1 puts the compose
// file in a directory of its own, so the parent is the operator's home
// directory: as a build context it is sent whole to the Docker daemon - SSH
// keys and mail included, and .dockerignore does not apply, being read from
// the context root - and the build then fails on a missing build/Dockerfile.
// ./source fails fast, naming itself.
func TestComposeBuildContextDefaultIsNotTheParentDirectory(t *testing.T) {
	b, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("context: ${SELFPOST_SOURCE:-./source}")) {
		t.Error("the build context's default is not ./source; a parent directory as the context is sent whole to the daemon")
	}
}
