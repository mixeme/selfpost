package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/store"
)

// A restore that goes wrong half way must leave the operator where they
// started: the archive is refused before the database is opened, and a start
// that fails after the guard is a restore again the next time (audit
// 2026-10-12, M1 and stage 3).

// A start that fails after the guard - here its Resync - leaves the manifest,
// so the next start is a restore again and runs the Resync the failed one did
// not finish. The failed start had the database open, so its files are no
// longer the archive's; the next start does not compare them again.
func TestARestoreStartThatFailsIsARestoreAgain(t *testing.T) {
	target := t.TempDir()
	extract(t, archiveOf(t, seedPanelProject(t)), target)
	cfg := panelConfig(t, target)

	broken := errors.New("postfix reload failed")
	if _, _, err := openData(cfg, func(*store.Store) error { return broken }); !errors.Is(err, broken) {
		t.Fatalf("a start whose Resync failed = %v, want that failure", err)
	}
	if _, err := os.Stat(cfg.manifestPath); err != nil {
		t.Fatalf("the failed start consumed the manifest: %v", err)
	}

	mapPath := filepath.Join(cfg.dataDir, "postfix", "sender_login_maps")
	if err := os.WriteFile(mapPath, []byte("stale map\n"), 0o640); err != nil {
		t.Fatalf("write stale map: %v", err)
	}
	st, restored, err := openData(cfg, testResync(cfg))
	if err != nil {
		t.Fatalf("the start after a failed one: %v", err)
	}
	st.Close()
	if !restored {
		t.Fatal("the start after a failed one did not take the data directory for a restore")
	}
	if got, _ := os.ReadFile(mapPath); string(got) != "@"+restoreDomain+" shop\n" {
		t.Errorf("sender map = %q: the second start did not run the Resync", got)
	}
	for _, name := range []string{backup.ManifestName, backup.ContentsName, "contents.checked.json"} {
		if _, err := os.Stat(filepath.Join(cfg.dataDir, name)); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the restore start succeeded (stat err = %v)", name, err)
		}
	}

	st, restored, err = openData(cfg, func(*store.Store) error {
		t.Error("an ordinary start ran the restore Resync")
		return nil
	})
	if err != nil {
		t.Fatalf("the ordinary start after the restore: %v", err)
	}
	st.Close()
	if restored {
		t.Error("the start after a finished restore took it for a restore again")
	}
}

// An archive unpacked in part - a download cut short, a tar stopped by a
// damaged block - is refused before the database is opened, with a message
// that says so, and nothing in the directory is changed: the database the
// archive left is as it left it, and the manifest is there for the next try.
func TestAnArchiveUnpackedInPartIsRefused(t *testing.T) {
	archive := archiveOf(t, seedPanelProject(t))
	dbSize := entrySize(t, archive, backup.DataArchivePrefix+"selfpost.db")
	for _, c := range []struct {
		name string
		cut  string
		at   int64
	}{
		{"in the database", backup.DataArchivePrefix + "selfpost.db", dbSize / 2},
		{"before the deploy files", backup.ComposeFileName, 0},
		{"in its last entry", backup.DataArchivePrefix + backup.ContentsName, 10},
	} {
		t.Run(c.name, func(t *testing.T) {
			target := t.TempDir()
			extractCut(t, archive, target, c.cut, c.at)
			cfg := panelConfig(t, target)
			before, err := os.ReadFile(cfg.dbPath)
			if err != nil {
				t.Fatalf("the cut archive left no database: %v", err)
			}

			_, _, err = openData(cfg, func(*store.Store) error {
				t.Error("a refused restore ran its Resync")
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "not unpacked whole") {
				t.Fatalf("the start on an archive cut %s = %v, want it refused as unpacked in part", c.name, err)
			}

			if after, _ := os.ReadFile(cfg.dbPath); !bytes.Equal(before, after) {
				t.Error("the refused start changed the database")
			}
			for _, side := range []string{"-wal", "-shm", "-journal"} {
				if _, err := os.Stat(cfg.dbPath + side); !os.IsNotExist(err) {
					t.Errorf("selfpost.db%s exists: the refused start opened the database", side)
				}
			}
			if _, err := os.Stat(cfg.manifestPath); err != nil {
				t.Errorf("the refused start removed the manifest: %v", err)
			}
		})
	}
}

// A file the archive holds and the data directory holds otherwise - damaged
// on the way, or unpacked from another archive - is refused by its name.
func TestADamagedRestoredFileIsRefusedByName(t *testing.T) {
	target := t.TempDir()
	extract(t, archiveOf(t, seedPanelProject(t)), target)
	cfg := panelConfig(t, target)

	key := filepath.Join(cfg.dataDir, "dkim", "keys", restoreDomain, "selfpost.private")
	f, err := os.OpenFile(key, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open key: %v", err)
	}
	f.WriteString("x")
	f.Close()

	_, _, err = openData(cfg, func(*store.Store) error {
		t.Error("a refused restore ran its Resync")
		return nil
	})
	want := "data/dkim/keys/" + restoreDomain + "/selfpost.private"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("the start on a damaged key = %v, want a refusal naming %s", err, want)
	}
}

// archiveOf takes a full backup of a seeded project for this binary's version.
func archiveOf(t *testing.T, deployRoot string) []byte {
	t.Helper()
	dataDir := filepath.Join(deployRoot, "data")
	var archive bytes.Buffer
	if err := backup.Create(&archive, backup.Params{
		DataDir:    dataDir,
		DBPath:     filepath.Join(dataDir, "selfpost.db"),
		Version:    buildinfo.Version,
		DeployRoot: deployRoot,
	}); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	return archive.Bytes()
}

// entrySize is the size of the archive's entry name.
func entrySize(t *testing.T, archive []byte, name string) int64 {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			t.Fatalf("the archive has no %s", name)
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		if hdr.Name == name {
			return hdr.Size
		}
	}
}
