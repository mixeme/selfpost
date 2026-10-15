package supervisor

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// socketPath is supervisord's control socket: build/supervisord.conf,
// [unix_http_server] file=, group selfpost, which the panel is in.
const socketPath = "/run/supervisor.sock"

// runOnceFor bounds RunOnce: a reload that is still running after it is
// reported as a failure. "postfix reload" checks that master runs, moves the
// incoming queue to active and sends master SIGHUP (Postfix 3.10.13,
// conf/postfix-script, "reload") - well under a second on the stand.
var runOnceFor = time.Minute

// pollEvery is how often RunOnce asks how the run is going; supervisord's own
// deferred start answers on the same beat (supervisor 4.2.5,
// supervisor/rpcinterface.py, startProcess(): onwait.delay = 0.05).
var pollEvery = 50 * time.Millisecond

// supervisord's process states (supervisor 4.2.5, supervisor/states.py,
// ProcessStates) as getProcessInfo returns them in 'state'.
const (
	stateStopped  = 0
	stateStarting = 10
	stateRunning  = 20
	stateBackoff  = 30
	stateStopping = 40
	stateExited   = 100
)

// faultAlreadyStarted is the fault startProcess raises for a program in
// STARTING, RUNNING or BACKOFF (supervisor 4.2.5, supervisor/xmlrpc.py,
// Faults.ALREADY_STARTED; rpcinterface.py, startProcess(): RUNNING_STATES).
const faultAlreadyStarted = 60

// RunOnce runs a one-shot program to its end and reports how it ended: nil
// only when it exited with status 0.
//
// "supervisorctl start" cannot tell that. For a program with startsecs=0 it
// returns as soon as the program is RUNNING - spawned, not finished - and its
// exit status is never reported (supervisor 4.2.5, supervisor/rpcinterface.py,
// startProcess(): process.transition() moves STARTING to RUNNING at once,
// then "return True"; assumptions B-U2). So RunOnce speaks supervisord's
// XML-RPC interface itself: startProcess without waiting, then
// getProcessInfo until the program leaves STARTING and RUNNING, and the
// 'exitstatus' supervisord kept (process.py, Subprocess.finish(): with
// startsecs=0 every end is EXITED, with its status).
//
// A run already in progress started before the caller's change was written,
// and may have read the old files: RunOnce waits for it to end and then starts
// its own. A run someone else starts after this one ended is newer than the
// caller's change, so its end serves just as well.
func RunOnce(program string) error {
	return newRPC(socketPath).runOnce(program)
}

type rpc struct {
	client *http.Client
}

func newRPC(sock string) *rpc {
	return &rpc{client: &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
			DisableKeepAlives: true,
		},
	}}
}

func (c *rpc) runOnce(program string) error {
	deadline := time.Now().Add(runOnceFor)
	for {
		_, err := c.call("supervisor.startProcess", program, false)
		var f *Fault
		if errors.As(err, &f) && f.Code == faultAlreadyStarted {
			info, err := c.waitEnd(program, deadline)
			if err != nil {
				return err
			}
			if info.state == stateBackoff {
				// Never for startsecs=0 (Subprocess.finish()), so no
				// start of ours could follow it.
				return fmt.Errorf("supervisor: %s is in BACKOFF: %s", program, info.spawnErr)
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("supervisor: %s did not come free within %s", program, runOnceFor)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("supervisor: start %s: %w", program, err)
		}
		break
	}
	info, err := c.waitEnd(program, deadline)
	if err != nil {
		return err
	}
	if info.state == stateExited && info.exitStatus == 0 {
		return nil
	}
	return &EndError{Program: program, State: info.stateName, Status: info.exitStatus, SpawnErr: info.spawnErr}
}

// EndError is a run of RunOnce that did not end EXITED with status 0: the
// state it ended in, its exit status, and supervisord's account of a failed
// spawn.
type EndError struct {
	Program  string
	State    string
	Status   int
	SpawnErr string
}

func (e *EndError) Error() string {
	msg := fmt.Sprintf("supervisor: %s ended %s, exit status %d", e.Program, e.State, e.Status)
	if e.SpawnErr != "" {
		msg += ": " + e.SpawnErr
	}
	return msg
}

// processInfo is the part of getProcessInfo's struct RunOnce reads.
type processInfo struct {
	state      int
	stateName  string
	exitStatus int
	spawnErr   string
}

// waitEnd asks for the program's state until it is out of STARTING, RUNNING
// and STOPPING.
func (c *rpc) waitEnd(program string, deadline time.Time) (processInfo, error) {
	for {
		v, err := c.call("supervisor.getProcessInfo", program)
		if err != nil {
			return processInfo{}, fmt.Errorf("supervisor: state of %s: %w", program, err)
		}
		m := v.members()
		info := processInfo{
			state:      m["state"].int(),
			stateName:  m["statename"].str(),
			exitStatus: m["exitstatus"].int(),
			spawnErr:   m["spawnerr"].str(),
		}
		switch info.state {
		case stateStarting, stateRunning, stateStopping:
		default:
			return info, nil
		}
		if time.Now().After(deadline) {
			return info, fmt.Errorf("supervisor: %s still %s after %s", program, info.stateName, runOnceFor)
		}
		time.Sleep(pollEvery)
	}
}

// Fault is an XML-RPC fault supervisord answered with (supervisor 4.2.5,
// supervisor/xmlrpc.py, Faults; the string is "<NAME>: <detail>").
type Fault struct {
	Code   int
	String string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("fault %d: %s", f.Code, f.String)
}

// call makes one XML-RPC call; params are strings and booleans, all the
// methods used here take.
func (c *rpc) call(method string, params ...any) (xvalue, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	xml.EscapeText(&b, []byte(method))
	b.WriteString(`</methodName><params>`)
	for _, p := range params {
		b.WriteString(`<param><value>`)
		switch v := p.(type) {
		case string:
			b.WriteString(`<string>`)
			xml.EscapeText(&b, []byte(v))
			b.WriteString(`</string>`)
		case bool:
			if v {
				b.WriteString(`<boolean>1</boolean>`)
			} else {
				b.WriteString(`<boolean>0</boolean>`)
			}
		default:
			return xvalue{}, fmt.Errorf("xml-rpc: parameter of type %T", p)
		}
		b.WriteString(`</value></param>`)
	}
	b.WriteString(`</params></methodCall>`)

	// supervisor 4.2.5, supervisor/xmlrpc.py, supervisor_xmlrpc_handler:
	// path '/RPC2'; the host is not looked at.
	resp, err := c.client.Post("http://localhost/RPC2", "text/xml", &b)
	if err != nil {
		return xvalue{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return xvalue{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return xvalue{}, fmt.Errorf("xml-rpc %s: HTTP %d", method, resp.StatusCode)
	}
	var r struct {
		Params []xvalue `xml:"params>param>value"`
		Fault  *xvalue  `xml:"fault>value"`
	}
	if err := xml.Unmarshal(body, &r); err != nil {
		return xvalue{}, fmt.Errorf("xml-rpc %s: %w", method, err)
	}
	if r.Fault != nil {
		m := r.Fault.members()
		return xvalue{}, &Fault{Code: m["faultCode"].int(), String: m["faultString"].str()}
	}
	if len(r.Params) != 1 {
		return xvalue{}, fmt.Errorf("xml-rpc %s: %d values in the answer", method, len(r.Params))
	}
	return r.Params[0], nil
}

// xvalue is an XML-RPC <value> as Python's xmlrpc.client writes it: a typed
// element, or bare text for a string.
type xvalue struct {
	Int     *string `xml:"int"`
	I4      *string `xml:"i4"`
	Boolean *string `xml:"boolean"`
	String  *string `xml:"string"`
	Struct  *struct {
		Members []struct {
			Name  string `xml:"name"`
			Value xvalue `xml:"value"`
		} `xml:"member"`
	} `xml:"struct"`
	Text string `xml:",chardata"`
}

func (v xvalue) members() map[string]xvalue {
	m := map[string]xvalue{}
	if v.Struct != nil {
		for _, mb := range v.Struct.Members {
			m[mb.Name] = mb.Value
		}
	}
	return m
}

func (v xvalue) int() int {
	for _, s := range []*string{v.Int, v.I4, v.Boolean} {
		if s != nil {
			n, _ := strconv.Atoi(strings.TrimSpace(*s))
			return n
		}
	}
	return 0
}

func (v xvalue) str() string {
	if v.String != nil {
		return *v.String
	}
	return v.Text
}
