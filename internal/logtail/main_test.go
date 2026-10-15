package logtail

import (
	"os"
	"testing"
	"time"
)

// TestMain reads mail.log stamps in UTC, whatever the zone of the machine the
// tests run on: Postfix 3.10 writes "%b %d %H:%M:%S" in the logging process's
// local time (src/util/msg_logger.c, msg_logger_print()) and lineTime reads it
// in time.Local, which in the container is the zone the fixtures are written
// in.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}
