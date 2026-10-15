// Package syslogrelay passes what programs in the container send to syslog(3)
// on to standard output, where supervisord takes it into the container's log.
//
// OpenDKIM logs through syslog(3) and nothing else (OpenDKIM 2.11.0~beta2,
// opendkim/opendkim.conf.5.in: Syslog, SyslogFacility and SyslogName are the
// only logging options; opendkim/opendkim.c calls openlog() and syslog()).
// syslog(3) sends each message as a datagram to /dev/log, and in the container
// nothing listened there: OpenDKIM's errors - a key it cannot read, a table it
// cannot open - went nowhere (audit 2026-10-12, L30). Postfix is not among the
// senders: it logs through its own postlogd (build/postfix-config.sh).
package syslogrelay

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
)

// stamp is the BSD syslog timestamp that follows the priority, "Oct 15
// 09:00:00 " (RFC 3164 § 4.1.2: "Mmm dd hh:mm:ss", the day padded with a
// space). The container's log has times of its own.
var stamp = regexp.MustCompile(`^[A-Z][a-z]{2} [ 0-9][0-9] [0-9]{2}:[0-9]{2}:[0-9]{2} `)

// Line turns one datagram into the line written out: the priority and the
// timestamp taken off, trailing newlines too; what remains is "ident[pid]:
// message". A severity of error or worse (RFC 3164 § 4.1.1: the priority is
// facility*8 + severity, and 0-3 are emergency to error) is said in front.
func Line(datagram []byte) string {
	b := bytes.TrimRight(datagram, "\r\n\x00")
	severity := -1
	if len(b) > 2 && b[0] == '<' {
		if end := bytes.IndexByte(b, '>'); end > 1 && end <= 4 {
			pri := 0
			ok := true
			for _, c := range b[1:end] {
				if c < '0' || c > '9' {
					ok = false
					break
				}
				pri = pri*10 + int(c-'0')
			}
			if ok {
				severity = pri % 8
				b = b[end+1:]
			}
		}
	}
	if loc := stamp.FindIndex(b); loc != nil {
		b = b[loc[1]:]
	}
	line := string(b)
	if severity >= 0 && severity <= 3 {
		line = "error: " + line
	}
	return line
}

// Run listens on a datagram socket at path, which anyone in the container may
// write to, and writes each message to w as a line until the socket fails.
// /dev/log is a link to path (build/entrypoint.sh).
func Run(path string, w io.Writer) error {
	_ = os.Remove(path)
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("syslog relay: listen on %s: %w", path, err)
	}
	defer conn.Close()
	// OpenDKIM runs as its own user; the socket is the container's local log,
	// as /dev/log is on a host.
	if err := os.Chmod(path, 0o666); err != nil {
		return fmt.Errorf("syslog relay: %w", err)
	}
	buf := make([]byte, 64<<10)
	for {
		n, _, err := conn.ReadFromUnix(buf)
		if err != nil {
			return fmt.Errorf("syslog relay: read: %w", err)
		}
		if n == 0 {
			continue
		}
		fmt.Fprintln(w, Line(buf[:n]))
	}
}
