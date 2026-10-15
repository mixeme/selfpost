package backup

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FormatContents identifies the record of what an archive's data directory
// holds.
const FormatContents = "selfpost-backup-contents"

// ContentsName is the record of the data directory's files, the last entry of
// every archive (data/contents.json). The manifest is the first entry and
// says a restore is under way; this one says the archive was unpacked to its
// end. A download cut short, or a tar that stopped at a damaged block, leaves
// a data directory with the manifest and without this file, and the panel
// refuses to start on it instead of opening half a database (audit
// 2026-10-12, stage 3).
const ContentsName = "contents.json"

// contentsCheckedName is ContentsName once a start has checked the files
// against it. The check is made once: the start that made it opens the
// database, and from then on the files are the panel's to change, so a start
// after a failed one (manifest still there) must not compare them again.
const contentsCheckedName = "contents.checked.json"

// Contents is the record written as ContentsName.
type Contents struct {
	Format string    `json:"format"`
	Files  []FileSum `json:"files"`
}

// FileSum is one regular file of the data directory as the archive holds it,
// named as in the archive (data/...).
type FileSum struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// archive is the tar stream a backup is written to, and the record of the
// data directory's files written to it so far. A nil *archive writes
// nothing: every file is opened and closed, which is how Check finds what
// cannot be read.
//
// Only the data directory is recorded. The deploy files are the operator's to
// edit before the first start - the guide sends them to .env for a new
// hostname and to docker-compose.yml for the tag - and the data directory is
// what the panel opens.
type archive struct {
	tw    *tar.Writer
	files []FileSum
}

// writeFile writes one regular file's header and content, read from r, and
// records it when it belongs to the data directory.
func (a *archive) writeFile(hdr *tar.Header, r io.Reader) error {
	if err := a.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write header %s: %w", hdr.Name, err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(a.tw, h), r)
	if err != nil {
		return fmt.Errorf("backup: copy %s: %w", hdr.Name, err)
	}
	if strings.HasPrefix(hdr.Name, DataArchivePrefix) {
		a.files = append(a.files, FileSum{Name: hdr.Name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	return nil
}

// writeContents ends the archive's data with the record of what it holds.
func (a *archive) writeContents() error {
	body, err := json.MarshalIndent(Contents{Format: FormatContents, Files: a.files}, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode contents: %w", err)
	}
	return writeTarBytes(&archive{tw: a.tw}, DataArchivePrefix+ContentsName, 0o600, body)
}

// errUnpackedPart is what a restore that is not whole is refused with.
func errUnpackedPart(detail string) error {
	return fmt.Errorf("backup: the archive was not unpacked whole (%s) - it was cut short or is damaged; nothing was opened. Unpack it again, from an intact copy, into an empty directory", detail)
}

// checkContents compares the data directory with the archive's record, once
// per restore (see contentsCheckedName).
func checkContents(dataDir string) error {
	checked := filepath.Join(dataDir, contentsCheckedName)
	if c, err := readContents(checked); err == nil {
		// A start before this one checked the files. It is the same restore
		// when the manifest is still the one that record holds; a different
		// archive unpacked over the directory since is checked anew.
		if verifyFiles(dataDir, c, DataArchivePrefix+ManifestName) == nil {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	path := filepath.Join(dataDir, ContentsName)
	c, err := readContents(path)
	if errors.Is(err, os.ErrNotExist) {
		return errUnpackedPart(DataArchivePrefix + ContentsName + ", its last entry, is not there")
	}
	if err != nil {
		return err
	}
	if err := verifyFiles(dataDir, c, ""); err != nil {
		return err
	}
	if err := os.Rename(path, checked); err != nil {
		return fmt.Errorf("backup: mark the restored files checked: %w", err)
	}
	return nil
}

// readContents reads a record of the archive's files.
func readContents(path string) (Contents, error) {
	var c Contents
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil || c.Format != FormatContents {
		return c, errUnpackedPart(filepath.Base(path) + " is not a record of the archive's files")
	}
	return c, nil
}

// verifyFiles compares the files c records with those under dataDir: each
// present, of its size, with its digest. A non-empty only limits it to the
// file of that name.
func verifyFiles(dataDir string, c Contents, only string) error {
	found := false
	for _, f := range c.Files {
		if only != "" && f.Name != only {
			continue
		}
		found = true
		rel, ok := strings.CutPrefix(f.Name, DataArchivePrefix)
		if !ok || rel == "" || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			return errUnpackedPart("the record names " + f.Name + ", which is not a file of the data directory")
		}
		file, err := os.Open(filepath.Join(dataDir, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			return errUnpackedPart(f.Name + " is missing")
		}
		if err != nil {
			return fmt.Errorf("backup: check %s: %w", f.Name, err)
		}
		h := sha256.New()
		n, err := io.Copy(h, file)
		file.Close()
		if err != nil {
			return fmt.Errorf("backup: check %s: %w", f.Name, err)
		}
		if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return errUnpackedPart(f.Name + " is not the file the archive holds")
		}
	}
	if only != "" && !found {
		return errUnpackedPart("the record does not name " + only)
	}
	return nil
}
