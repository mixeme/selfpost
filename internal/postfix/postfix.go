// Package postfix owns the Postfix configuration files the panel edits at
// runtime and the privileged reload that applies them (architecture.md § Mail
// path, security.md): the smtpd_sender_login_maps table binding each
// application's SASL login to the sender addresses it may use, plus the relay
// configuration in main.cf.
package postfix

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mixeme/selfpost/internal/configsafe"
	"github.com/mixeme/selfpost/internal/supervisor"
)

// Postfix manages the on-disk Postfix state the panel is responsible for. After
// rewriting a map it asks Postfix to reload.
type Postfix struct {
	senderLoginMapsPath string

	// reload asks the running Postfix to re-read its configuration. It is a
	// field so tests can substitute a no-op; the default drives supervisord.
	reload func() error
}

// New builds a manager rooted at dir (typically /data/postfix), the same layout
// entrypoint.sh prepares. The default reload path signals Postfix through
// supervisord.
func New(dir string) *Postfix {
	return &Postfix{
		senderLoginMapsPath: filepath.Join(dir, "sender_login_maps"),
		reload:              reloadViaSupervisor,
	}
}

// SenderLoginMapsPath is the absolute path of the generated map, so main.cf
// can point smtpd_sender_login_maps at it.
func (p *Postfix) SenderLoginMapsPath() string {
	return p.senderLoginMapsPath
}

// Binding is one sender-address → login pair (architecture.md § Mail path).
// Address is either a domain wildcard "@example.com" or a specific address
// "alerts@example.com".
type Binding struct {
	Address string
	Login   string
}

// RebuildSenderLoginMaps regenerates the sender_login_maps file from the full
// set of bindings and reloads Postfix (architecture.md § Mail path). Full
// regeneration (rather than incremental edits) keeps the file a pure function
// of the registry, so add, edit and delete share one idempotent path. The file
// is written atomically before the reload.
//
// Several applications may be authorised for the same address (many-to-one,
// architecture.md § Mail path) — their logins are merged onto a single line as
// a comma-separated list, which is how Postfix expects multiple owners of one
// sender.
//
// Postfix 3.7.11, src/smtpd/smtpd_check.c, reject_auth_sender_login_mismatch():
// the value is split into owners with mystrtok(&cp, CHARS_COMMA_SP).
func (p *Postfix) RebuildSenderLoginMaps(bindings []Binding) error {
	content, err := renderSenderLoginMaps(bindings)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(p.senderLoginMapsPath, content, 0o640); err != nil {
		return err
	}
	return p.reload()
}

// Reload asks Postfix to re-read its configuration without regenerating any
// file. It backs the panel's manual reload button (architecture.md § Panel
// HTTP surface).
func (p *Postfix) Reload() error {
	return p.reload()
}

// SetReloadHook replaces how configuration is applied after a rebuild. Tests
// that cannot reach supervisord use this to verify file regeneration alone.
func (p *Postfix) SetReloadHook(fn func() error) {
	if fn != nil {
		p.reload = fn
	}
}

// renderSenderLoginMaps builds the sender_login_maps file contents. Keys are
// sorted for deterministic output and the logins under each key are sorted and
// de-duplicated. Every address and login is re-checked for injection safety
// before being written (security.md) — upstream validation already guarantees
// this, but the writer refuses to emit anything unsafe as a hard backstop.
func renderSenderLoginMaps(bindings []Binding) ([]byte, error) {
	byAddr := make(map[string][]string)
	order := make([]string, 0)
	for _, b := range bindings {
		if err := assertMapSafe(b.Address, b.Login); err != nil {
			return nil, err
		}
		// One line per key as Postfix folds it: smtpd opens the map with
		// DICT_FLAG_FOLD_FIX and no DICT_FLAG_DUP_* flag, so two keys that
		// differ only in case would make the whole table unusable (Postfix
		// 3.7.11, src/util/dict_thash.c, dict_thash_open()). The panel stores
		// addresses in lower case already; this keeps the file safe if a row
		// ever is not.
		addr := strings.ToLower(b.Address)
		if _, seen := byAddr[addr]; !seen {
			order = append(order, addr)
		}
		byAddr[addr] = appendUnique(byAddr[addr], b.Login)
	}
	sort.Strings(order)

	var sb strings.Builder
	for _, addr := range order {
		logins := byAddr[addr]
		sort.Strings(logins)
		// texthash format: <key><whitespace><value>. A comma-separated value lists
		// every login permitted to use this sender (architecture.md § Mail path).
		fmt.Fprintf(&sb, "%s %s\n", addr, strings.Join(logins, ","))
	}
	return []byte(sb.String()), nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// What may never reach a sender-map line: whitespace and the newline that would
// end it, the comma that separates values, the colon a lookup key cannot carry,
// and the backslash. A login additionally may not carry "@" — that would make it
// look like an address.
//
// Postfix 3.7.11, src/util/dict_thash.c, dict_thash_open(): the key ends at
// the first blank outside quotes, a backslash escapes the next character;
// src/util/readlline.c: a newline ends the line (one that starts with a blank
// continues the one before); the owners split on comma and blank (see
// RebuildSenderLoginMaps).
const (
	senderMapAddressForbidden = " \t\r\n,:\\"
	senderMapLoginForbidden   = " \t\r\n,:@\\"
)

// assertMapSafe rejects any address/login value that could break out of a single
// map line or inject a directive. Addresses are validated to a strict whitelist
// (letters, digits, '@', '.', '-', '_', '+') and logins to an even stricter one
// upstream (security.md); this is defence in depth against a validation gap ever
// letting whitespace, a newline or a comma (the value separator) through into
// the file (security.md).
func assertMapSafe(address, login string) error {
	if err := configsafe.Token("address", address, senderMapAddressForbidden); err != nil {
		return fmt.Errorf("postfix: %w", err)
	}
	if err := configsafe.Token("login", login, senderMapLoginForbidden); err != nil {
		return fmt.Errorf("postfix: %w", err)
	}
	return nil
}

// reloadViaSupervisor asks supervisord (PID 1, running as root) to run the
// one-shot `postfix-reload` program, which executes the canonical `postfix
// reload` and re-reads main.cf/master.cf and the lookup tables they reference.
// The panel runs unprivileged: it cannot run `postfix reload` itself, and it
// cannot signal the Postfix master directly because `postfix start-fg` forks a
// separate master whose PID supervisord does not track (a SIGHUP to the
// supervised process would never reach it). Going through supervisord's
// group-accessible control socket runs the reload as root without any panel
// privilege (architecture.md § Mail path, security.md).
//
// Arguments are fixed literals — no user input is interpolated into the command,
// and it never goes through a shell (security.md).
//
// Postfix 3.7.11, conf/postfix-script, start-fg: a process that is not PID 1
// runs "$daemon_directory/master -s" as its child, and src/master/master.c
// puts that master in a session of its own (setsid()). "reload" sends the
// master SIGHUP; src/master/master.c calls master_refresh(), and
// master_conf.c, master_config(), lets the running children "commit suicide
// whenever it is convenient" while new ones get the new configuration; a
// texthash table is read into memory once per process
// (README_FILES/DATABASE_README).
//
// The call returns when "postfix reload" has ended, with an error unless it
// exited 0 (a postsuper failure, a main.cf postfix(1) cannot read:
// conf/postfix-script, "reload"; src/postfix/postfix.c), which the page that
// saved shows. Before, supervisorctl's start returned as the reload was
// spawned, and a reload already running counted as done though it may have
// read the files before this change (supervisor.RunOnce; assumptions B-U2).
// A reload while no master runs - Postfix stopped, or still in its start,
// the wrapper waiting for the milter sockets - is no error: the program exits
// 75 then (build/postfix-reload.sh), and the master that starts next reads
// the files as they are now. Before, the program's state stood in for that,
// and for the whole of startsecs after a start any failed reload passed as
// one (review of stage 2c, F5).
func reloadViaSupervisor() error {
	err := supervisor.RunOnce("postfix-reload")
	var end *supervisor.EndError
	if errors.As(err, &end) && end.State == "EXITED" && end.Status == reloadNotRunning {
		log.Printf("postfix: no Postfix master runs; its start reads the files")
		return nil
	}
	return err
}

// reloadNotRunning is build/postfix-reload.sh's exit status when no master
// runs.
const reloadNotRunning = 75
