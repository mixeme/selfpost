package e2e

import (
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"time"
)

// port25Addr is where compose.override.yml publishes port 25: no TLS required,
// no AUTH offered - a stranger's mail server talking to this one.
const port25Addr = "127.0.0.1:20025"

// deliverPort25 is one transaction on port 25 the way another mail server makes
// it: HELO, MAIL, RCPT, DATA with message as it stands. The result says at
// which stage it was refused, like attemptSend's.
func deliverPort25(from, to, message string) sendResult {
	var res sendResult
	conn, err := net.DialTimeout("tcp", port25Addr, 10*time.Second)
	if err != nil {
		res.dialErr = fmt.Errorf("dial %s: %w", port25Addr, err)
		return res
	}
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		res.dialErr = fmt.Errorf("smtp handshake: %w", err)
		_ = conn.Close()
		return res
	}
	defer c.Close()
	if err := c.Hello("mx.stranger.e2e.test"); err != nil {
		res.dialErr = err
		return res
	}
	if err := c.Mail(from); err != nil {
		res.mailErr = err
		_ = c.Quit()
		return res
	}
	if err := c.Rcpt(to); err != nil {
		res.rcptErr = err
		_ = c.Quit()
		return res
	}
	w, err := c.Data()
	if err != nil {
		res.dataErr = err
		_ = c.Quit()
		return res
	}
	if _, err := w.Write([]byte(message)); err != nil {
		res.dataErr = err
	} else if err := w.Close(); err != nil {
		res.dataErr = err
	}
	_ = c.Quit()
	return res
}

// waitForPort25Ready polls port 25 until Postfix greets on it. A bare connect
// is not enough: Docker's proxy accepts on the published port whether or not
// anything listens behind it (seen on the stand, date not recorded - see
// waitForSMTPSReady).
func waitForPort25Ready(timeout time.Duration) error {
	return waitFor("port 25 to greet", timeout, 500*time.Millisecond, func() (bool, error) {
		conn, err := net.DialTimeout("tcp", port25Addr, 2*time.Second)
		if err != nil {
			return false, err
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 4)
		n, err := conn.Read(buf)
		if err != nil {
			return false, err
		}
		if string(buf[:n]) != "220 " {
			return false, fmt.Errorf("greeting starts with %q", buf[:n])
		}
		return true, nil
	})
}

// deliverPort25Raw is deliverPort25 with the DATA bytes written as they
// stand. net/smtp's data writer would turn a bare LF into CRLF and stuff a
// lone "." (net/textproto, DotWriter), which is the very input this is for.
// It returns an error for a reply other than the one each step expects.
func deliverPort25Raw(from, to string, data []byte) error {
	conn, err := net.DialTimeout("tcp", port25Addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", port25Addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	tp := textproto.NewConn(conn)
	expect := func(code int, after string) error {
		if _, _, err := tp.ReadResponse(code); err != nil {
			return fmt.Errorf("after %s: %w", after, err)
		}
		return nil
	}
	if err := expect(220, "connect"); err != nil {
		return err
	}
	for _, step := range []struct {
		cmd  string
		code int
	}{
		{"HELO mx.stranger.e2e.test", 250},
		{"MAIL FROM:<" + from + ">", 250},
		{"RCPT TO:<" + to + ">", 250},
		{"DATA", 354},
	} {
		if err := tp.PrintfLine("%s", step.cmd); err != nil {
			return fmt.Errorf("%s: %w", step.cmd, err)
		}
		if err := expect(step.code, step.cmd); err != nil {
			return err
		}
	}
	if _, err := tp.W.Write(data); err != nil {
		return fmt.Errorf("DATA content: %w", err)
	}
	if err := tp.W.Flush(); err != nil {
		return fmt.Errorf("DATA content: %w", err)
	}
	if err := expect(250, "end of data"); err != nil {
		return err
	}
	_ = tp.PrintfLine("QUIT")
	return nil
}
