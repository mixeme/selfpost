package e2e

import (
	"bytes"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-msgauth/dkim"
)

// senderDomain and recipient are fixed for the whole run: one sending domain
// registered in the panel, and the sink-MX as the single recipient every
// positive send targets. Here, not in main_test.go: smtp_client.go uses them,
// and the package builds without its tests (staticcheck reads it so).
const (
	senderDomain = "sender.e2e.test"
	recipient    = "rcpt@sink.e2e.test"
)

// findSinkMessage polls the sink-MX's dump directory (see test/e2e/sink) for
// a file whose contents contain token — the unique Subject-line marker every
// test send carries — and returns it. Polling a directory listing rather than
// sleeping a fixed duration is what keeps this deterministic even under a
// slow CI runner (plan C.4).
func findSinkMessage(stageDir, token string, timeout time.Duration) ([]byte, error) {
	dir := filepath.Join(stageDir, "mail-stage")
	var found []byte
	err := waitFor(fmt.Sprintf("sink-MX to receive a message tagged %q", token), timeout, 300*time.Millisecond, func() (bool, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if bytes.Contains(b, []byte(token)) {
				found = b
				return true, nil
			}
		}
		return false, fmt.Errorf("no dumped message contains %q yet (%d on disk)", token, len(entries))
	})
	return found, err
}

// verifyDKIM checks that raw (a message dumped by the sink) carries a DKIM
// signature for domain that validates against the public key published in the
// fake zone — i.e. exactly what a real receiver would check, using DNS the
// harness itself controls instead of net.DefaultResolver (plan C.4).
func verifyDKIM(raw []byte, domain string) error {
	verifications, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{
		LookupTXT: lookupTXTFunc(),
	})
	if err != nil {
		return fmt.Errorf("dkim verify: %w", err)
	}
	for _, v := range verifications {
		if v.Domain == domain {
			if v.Err != nil {
				return fmt.Errorf("dkim signature for %s did not validate: %w", domain, v.Err)
			}
			return nil
		}
	}
	return fmt.Errorf("no DKIM signature found for domain %s (got %d signature(s))", domain, len(verifications))
}

// postfixReceived returns the Received: headers of raw that a Postfix smtpd
// wrote - "(Postfix)" after the host name, the only form it writes (Postfix
// 3.10.13, src/smtpd/smtpd.c:3627-3645, "by %s (%s)" with var_mail_name).
// In the stand only SelfPost's Postfix writes one: the tests compose their
// messages without any, and the sink, smtp-sink, adds its own with "by
// <host> (smtp-sink)" (src/smtpstone/smtp-sink.c:520-531,
// mail_file_finish_header()), which the "(Postfix)" filter leaves out.
func postfixReceived(raw []byte) ([]string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse the delivered message: %w", err)
	}
	var out []string
	for _, v := range msg.Header["Received"] {
		if strings.Contains(v, "(Postfix)") {
			out = append(out, v)
		}
	}
	return out, nil
}

// checkReceivedHidden: the submission ports set smtpd_hide_client_session
// (build/postfix-config.sh), so SelfPost's Received: header is the redacted
// one - it opens with "by", names no client and no protocol (Postfix 3.10.13,
// src/smtpd/smtpd.c:3495, the "from" part written only when the parameter
// is off, and with_protocol emptied with it).
func checkReceivedHidden(raw []byte) error {
	recv, err := postfixReceived(raw)
	if err != nil {
		return err
	}
	if len(recv) != 1 {
		return fmt.Errorf("want one Received: header from SelfPost's Postfix, got %d: %q", len(recv), recv)
	}
	if !strings.HasPrefix(recv[0], "by ") || strings.Contains(recv[0], " with ") {
		return fmt.Errorf("the Received: header names the client session: %q", recv[0])
	}
	return nil
}

// connectFromPattern matches Postfix's smtpd "connect from ...[ADDR]" log
// line, which is how the harness learns the source address Postfix itself
// observed for a just-made connection (needed to configure a level-2 rate
// limit's allowed-IPs list — the address a host-published port is seen as
// inside the container depends on Docker's NAT and isn't worth hard-coding).
// Postfix 3.10.13, src/smtpd/smtpd.c, smtpd_service(): msg_info("connect from
// %s", namaddr), namaddr being "name[addr]".
var connectFromPattern = regexp.MustCompile(`connect from [^\[]*\[([0-9a-fA-F.:]+)\]`)

// lastSMTPClientIP reads mail.log inside the selfpost container and returns
// the most recent address Postfix's smtpd logged a connection from.
func lastSMTPClientIP(s *stack) (string, error) {
	out, err := s.execIn("selfpost", "tail", "-n", "200", "/data/log/mail.log")
	if err != nil {
		return "", err
	}
	matches := connectFromPattern.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("no \"connect from\" line in mail.log yet")
	}
	return matches[len(matches)-1][1], nil
}
