package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The environment the image reads and the environment the documents describe
// are compared as they are, not as lists kept by hand beside them (audit
// 2026-10-12, stage 5 - the three lists this file held were compared with each
// other and with nothing else). Read: what the panel's code asks of the
// environment (cmd/panel, cmd/selfpost-backup and internal, tests aside) and what the start-up
// scripts take with a default (build/*.sh, "${VAR:-…}"). Documented: the
// operator's table in guide.md § Environment variables, with the paragraph
// under it, and the internal list in architecture.md § Configuration.

var (
	goEnvRead = regexp.MustCompile(`(?:envDefault|envInt|envBool|envSeconds|envDuration|os\.Getenv|os\.LookupEnv)\("([A-Z][A-Z0-9_]+)"`)
	// A name kept in a constant, as the backup CLI keeps its password's.
	goEnvConst    = regexp.MustCompile(`\w+Env\s*=\s*"([A-Z][A-Z0-9_]+)"`)
	scriptEnvRead = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]+):[-?=]`)
	docEnvName    = regexp.MustCompile("`([A-Z][A-Z0-9_]+)`")
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// envRead is every variable the code and the scripts read.
func envRead(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, root := range []string{".", filepath.Join("..", "selfpost-backup"), filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			text := readFile(t, path)
			for _, m := range goEnvRead.FindAllStringSubmatch(text, -1) {
				keys[m[1]] = true
			}
			for _, m := range goEnvConst.FindAllStringSubmatch(text, -1) {
				keys[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	scripts, err := filepath.Glob(filepath.Join("..", "..", "build", "*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no build scripts: %v", err)
	}
	for _, path := range scripts {
		for _, m := range scriptEnvRead.FindAllStringSubmatch(readFile(t, path), -1) {
			keys[m[1]] = true
		}
	}
	return keys
}

// section is the text of doc from the first line that starts with start up
// to the next that starts with any of ends.
func section(t *testing.T, doc, start string, ends ...string) string {
	t.Helper()
	i := strings.Index(doc, "\n"+start)
	if i < 0 {
		t.Fatalf("no %q", start)
	}
	rest := doc[i+1+len(start):]
	end := len(rest)
	for _, e := range ends {
		if j := strings.Index(rest, "\n"+e); j >= 0 && j < end {
			end = j
		}
	}
	return rest[:end]
}

// envDocumented is every variable the documents name, and where.
func envDocumented(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{}
	guide := section(t, readFile(t, filepath.Join("..", "..", "docs", "guide.md")), "### Environment variables", "### ")
	for _, m := range docEnvName.FindAllStringSubmatch(guide, -1) {
		keys[m[1]] = "guide.md § Environment variables"
	}
	arch := section(t, readFile(t, filepath.Join("..", "..", "docs", "architecture.md")), "**Internal env vars.**", "### ", "## ", "**")
	for _, m := range docEnvName.FindAllStringSubmatch(arch, -1) {
		if _, ok := keys[m[1]]; !ok {
			keys[m[1]] = "architecture.md § Configuration"
		}
	}
	return keys
}

func TestEnvironmentReadIsDocumented(t *testing.T) {
	doc := envDocumented(t)
	var missing []string
	for key := range envRead(t) {
		if _, ok := doc[key]; !ok {
			missing = append(missing, key)
		}
	}
	slices.Sort(missing)
	for _, key := range missing {
		t.Errorf("%s is read but documented nowhere: guide.md § Environment variables for an operator's setting, architecture.md § Configuration for an internal one", key)
	}
}

func TestEnvironmentDocumentedIsRead(t *testing.T) {
	read := envRead(t)
	var stale []string
	for key, where := range envDocumented(t) {
		if !read[key] {
			stale = append(stale, key+" ("+where+")")
		}
	}
	slices.Sort(stale)
	for _, key := range stale {
		t.Errorf("%s is documented but nothing reads it", key)
	}
}

// The parsers find what they are for: a key of each kind is among the read.
func TestEnvironmentParsersFindKeys(t *testing.T) {
	read := envRead(t)
	for _, key := range []string{"SELFPOST_HOSTNAME", "PANEL_HTTP_ADDR", "MILTER_WAIT_TIMEOUT", "POSTFIX_QUEUE_DIR"} {
		if !read[key] {
			t.Errorf("%s not found among the keys read", key)
		}
	}
}
