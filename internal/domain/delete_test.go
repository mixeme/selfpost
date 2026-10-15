package domain

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// countingApps is fakeApps that counts the sender map's rebuilds and can
// fail them.
type countingApps struct {
	fakeApps
	resyncs   int
	resyncErr error
}

func (c *countingApps) Resync() error {
	c.resyncs++
	return c.resyncErr
}

// A deletion that fails after the domain's row is gone says the domain is
// deleted, and runs every rebuild anyway: the sender map was left naming the
// purged logins when the signing tables failed first (audit 2026-10-12, L17).
// The key stays while the selector table may still name it.
func TestDeleteRunsEveryRebuildAndSaysTheDomainIsGone(t *testing.T) {
	apps := &countingApps{}
	s, keys := newTestService(t, apps)
	d, err := s.Add("gone.example")
	if err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(keys.keysDir, d.Name)
	apps.resyncs = 0

	// A directory where the table goes: no file can be renamed over it.
	if err := os.Remove(keys.selectorsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(keys.selectorsPath, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = s.Delete(d.ID)
	if !errors.Is(err, ErrDeletedIncomplete) {
		t.Fatalf("Delete with the selector table unwritable = %v, want ErrDeletedIncomplete", err)
	}
	if _, err := s.Get(d.ID); !errors.Is(err, store.ErrDomainNotFound) {
		t.Errorf("the domain is still in the registry: %v", err)
	}
	if apps.resyncs != 1 {
		t.Errorf("the sender map was rebuilt %d times, want once", apps.resyncs)
	}
	if _, err := os.Stat(keyDir); err != nil {
		t.Errorf("the key went while the tables may still name it: %v", err)
	}

	// Reload configuration finishes the deletion: the tables rebuilt, the
	// key of a domain that is gone goes too; the others' stay.
	if err := os.RemoveAll(keys.selectorsPath); err != nil {
		t.Fatal(err)
	}
	keep, err := s.Add("kept.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resync(); err != nil {
		t.Fatalf("Resync: %v", err)
	}
	if _, err := os.Stat(keyDir); !os.IsNotExist(err) {
		t.Errorf("the deleted domain's key is still there after Resync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keys.keysDir, keep.Name)); err != nil {
		t.Errorf("Resync removed a domain's key: %v", err)
	}
}

// The sender map failing is the same: deleted, said so; the tables were
// rebuilt, so the key goes.
func TestDeleteWithTheSenderMapFailing(t *testing.T) {
	apps := &countingApps{resyncErr: errors.New("postfix away")}
	s, keys := newTestService(t, apps)
	d, err := s.Add("gone.example")
	if err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(keys.keysDir, d.Name)
	if err := s.Delete(d.ID); !errors.Is(err, ErrDeletedIncomplete) {
		t.Fatalf("Delete with the sender map failing = %v, want ErrDeletedIncomplete", err)
	}
	if _, err := os.Stat(keyDir); !os.IsNotExist(err) {
		t.Errorf("the key is still there after the table was rebuilt: %v", err)
	}
}
