package view

import (
	"os"
	"testing"
	"time"
)

// TestMain runs the tests in UTC, whatever the zone of the machine they run
// on: the panel shows times and counts days in time.Local, the container's
// zone (TZ), and the fixtures are written in UTC. The tests of another zone
// set it themselves (inZone).
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// inZone runs the rest of the test in the zone name, as a container started
// with TZ=name; the test must not be parallel.
func inZone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no zone %s here: %v", name, err)
	}
	time.Local = loc
	t.Cleanup(func() { time.Local = time.UTC })
}
