package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LogtailState is the persisted read position of the mail.log tailer: how far
// into the log it had got, and a fingerprint of the file that offset refers to.
// It lets the tailer resume after a panel restart instead of jumping to
// end-of-file and losing the delivery lines written while it was down.
type LogtailState struct {
	Fingerprint string
	Offset      int64
}

// LogtailState returns the stored read position for path. ok is false the first
// time a path is followed (nothing persisted yet), which the tailer treats as
// "start at the end".
func (s *Store) LogtailState(path string) (LogtailState, bool, error) {
	var st LogtailState
	err := s.db.QueryRow(
		`SELECT fingerprint, read_offset FROM logtail_state WHERE path = ?`,
		path,
	).Scan(&st.Fingerprint, &st.Offset)
	if errors.Is(err, sql.ErrNoRows) {
		return LogtailState{}, false, nil
	}
	if err != nil {
		return LogtailState{}, false, fmt.Errorf("read logtail state for %q: %w", path, err)
	}
	return st, true, nil
}

// SaveLogtailState records the tailer's read position for path, replacing any
// previous one. It is called on a timer while tailing, so it is a single small
// upsert rather than a transaction.
func (s *Store) SaveLogtailState(path string, st LogtailState) error {
	_, err := s.db.Exec(
		`INSERT INTO logtail_state (path, fingerprint, read_offset, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		     fingerprint = excluded.fingerprint,
		     read_offset = excluded.read_offset,
		     updated_at  = excluded.updated_at`,
		path, st.Fingerprint, st.Offset, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save logtail state for %q: %w", path, err)
	}
	return nil
}
