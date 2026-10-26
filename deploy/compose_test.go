package deploy

import (
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
