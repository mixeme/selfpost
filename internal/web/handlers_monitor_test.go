package web

import (
	"path/filepath"
	"testing"
)

// After log rotation renames mail.log away, Postfix takes about a second to
// recreate it on reload (spec B.2); a missing file in that window is a normal,
// transient gap, not an operator-facing failure.
func TestReadLogTailMissingFileIsNotAnError(t *testing.T) {
	s := &Server{cfg: Config{MailLogPath: filepath.Join(t.TempDir(), "mail.log")}}

	lines, errText := s.readLogTail()
	if lines != nil {
		t.Errorf("lines = %v, want nil", lines)
	}
	if errText != "" {
		t.Errorf("errText = %q, want empty (missing file is not an error)", errText)
	}
}
