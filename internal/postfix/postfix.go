// Package postfix owns the Postfix configuration files the panel edits at
// runtime and the privileged reload that applies them (spec 5.1, 7.6.3-4). In
// Phase 4 that is the smtpd_sender_login_maps table binding each application's
// SASL login to the sender addresses it may use; the full relay configuration
// lands in Phase 5.
package postfix

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

// SenderLoginMapsPath is the absolute path of the generated map, so the Postfix
// main.cf written in Phase 5 can point smtpd_sender_login_maps at it.
func (p *Postfix) SenderLoginMapsPath() string {
	return p.senderLoginMapsPath
}

// Binding is one sender-address → login pair (spec 5.1). Address is either a
// domain wildcard "@example.com" or a specific address "alerts@example.com".
type Binding struct {
	Address string
	Login   string
}

// RebuildSenderLoginMaps regenerates the sender_login_maps file from the full
// set of bindings and reloads Postfix (spec 5.1). Full regeneration (rather than
// incremental edits) keeps the file a pure function of the registry, so add,
// edit and delete share one idempotent path. The file is written atomically
// before the reload.
//
// Several applications may be authorised for the same address (many-to-one,
// spec 5.1 §4) — their logins are merged onto a single line as a comma-separated
// list, which is how Postfix expects multiple owners of one sender.
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
// file. It backs the panel's manual reload button (spec 7.2.12).
func (p *Postfix) Reload() error {
	return p.reload()
}

// renderSenderLoginMaps builds the sender_login_maps file contents. Keys are
// sorted for deterministic output and the logins under each key are sorted and
// de-duplicated. Every address and login is re-checked for injection safety
// before being written (spec 7.6.4) — upstream validation already guarantees
// this, but the writer refuses to emit anything unsafe as a hard backstop.
func renderSenderLoginMaps(bindings []Binding) ([]byte, error) {
	byAddr := make(map[string][]string)
	order := make([]string, 0)
	for _, b := range bindings {
		if err := assertMapSafe(b.Address, b.Login); err != nil {
			return nil, err
		}
		if _, seen := byAddr[b.Address]; !seen {
			order = append(order, b.Address)
		}
		byAddr[b.Address] = appendUnique(byAddr[b.Address], b.Login)
	}
	sort.Strings(order)

	var sb strings.Builder
	for _, addr := range order {
		logins := byAddr[addr]
		sort.Strings(logins)
		// texthash format: <key><whitespace><value>. A comma-separated value
		// lists every login permitted to use this sender (spec 5.1 §4).
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

// assertMapSafe rejects any address/login value that could break out of a single
// map line or inject a directive. Addresses are validated to a strict whitelist
// (letters, digits, '@', '.', '-', '_', '+') and logins to an even stricter one
// upstream (spec 7.6.2); this is defence in depth against a validation gap ever
// letting whitespace, a newline or a comma (the value separator) through into
// the file (spec 7.6.4).
func assertMapSafe(address, login string) error {
	if address == "" || login == "" {
		return fmt.Errorf("postfix: empty address or login")
	}
	if strings.ContainsAny(address, " \t\r\n,:\\") {
		return fmt.Errorf("postfix: unsafe character in address %q", address)
	}
	if strings.ContainsAny(login, " \t\r\n,:@\\") {
		return fmt.Errorf("postfix: unsafe character in login %q", login)
	}
	return nil
}

// reloadViaSupervisor asks supervisord (PID 1, running as root) to run the
// one-shot `postfix-reload` program, which executes the canonical
// `postfix reload` and re-reads main.cf/master.cf and the lookup tables they
// reference. The panel runs unprivileged: it cannot run `postfix reload` itself,
// and it cannot signal the Postfix master directly because `postfix start-fg`
// forks a separate master whose PID supervisord does not track (a SIGHUP to the
// supervised process would never reach it). Going through supervisord's
// group-accessible control socket runs the reload as root without any panel
// privilege (spec 5.2, 7.2.12, 7.6.3, 7.6.8).
//
// Arguments are fixed literals — no user input is interpolated into the command,
// and it never goes through a shell (spec 7.6.3).
func reloadViaSupervisor() error {
	cmd := exec.Command("supervisorctl",
		"-c", "/etc/supervisor/supervisord.conf",
		"start", "postfix-reload")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// A reload already in flight is not a failure: that pending run reloads
		// Postfix after our file is in place (the file is written before this).
		if strings.Contains(string(out), "already started") {
			return nil
		}
		return fmt.Errorf("reload postfix via supervisor: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
