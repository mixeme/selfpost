package supervisor

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSupervisord answers startProcess and getProcessInfo the way supervisor
// 4.2.5 does over XML-RPC (Python's xmlrpc.client.dumps with methodresponse:
// one <param>, or a <fault> struct), from a script of states.
type fakeSupervisord struct {
	mu sync.Mutex
	// alreadyStarted is how many startProcess calls get fault 60 first.
	alreadyStarted int
	// states are the (state, statename, exitstatus) getProcessInfo returns,
	// one per call; the last one repeats.
	states []fakeState
	starts int
	asked  int
}

type fakeState struct {
	state      int
	name       string
	exitStatus int
}

var methodRe = regexp.MustCompile(`<methodName>([^<]+)</methodName>`)

func (f *fakeSupervisord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m := methodRe.FindSubmatch(body)
	if r.URL.Path != "/RPC2" || m == nil {
		http.Error(w, "no", http.StatusNotFound)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "text/xml")
	switch string(m[1]) {
	case "supervisor.startProcess":
		f.starts++
		if !strings.Contains(string(body), "<boolean>0</boolean>") {
			http.Error(w, "start must not wait", http.StatusBadRequest)
			return
		}
		if f.alreadyStarted > 0 {
			f.alreadyStarted--
			io.WriteString(w, "<?xml version='1.0'?>\n<methodResponse>\n<fault>\n<value><struct>\n"+
				"<member>\n<name>faultCode</name>\n<value><int>60</int></value>\n</member>\n"+
				"<member>\n<name>faultString</name>\n<value><string>ALREADY_STARTED: postfix-reload</string></value>\n</member>\n"+
				"</struct></value>\n</fault>\n</methodResponse>\n")
			return
		}
		io.WriteString(w, "<?xml version='1.0'?>\n<methodResponse>\n<params>\n<param>\n<value><boolean>1</boolean></value>\n</param>\n</params>\n</methodResponse>\n")
	case "supervisor.getProcessInfo":
		s := f.states[min(f.asked, len(f.states)-1)]
		f.asked++
		fmt.Fprintf(w, "<?xml version='1.0'?>\n<methodResponse>\n<params>\n<param>\n<value><struct>\n"+
			"<member>\n<name>name</name>\n<value><string>postfix-reload</string></value>\n</member>\n"+
			"<member>\n<name>state</name>\n<value><int>%d</int></value>\n</member>\n"+
			"<member>\n<name>statename</name>\n<value><string>%s</string></value>\n</member>\n"+
			"<member>\n<name>exitstatus</name>\n<value><int>%d</int></value>\n</member>\n"+
			"<member>\n<name>spawnerr</name>\n<value><string></string></value>\n</member>\n"+
			"</struct></value>\n</param>\n</params>\n</methodResponse>\n", s.state, s.name, s.exitStatus)
	default:
		http.Error(w, "unknown method", http.StatusBadRequest)
	}
}

func serveFake(t *testing.T, f *fakeSupervisord) *rpc {
	t.Helper()
	// A short directory: a unix socket's path is limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "sv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets: %v", err)
	}
	srv := &http.Server{Handler: f}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	old := pollEvery
	pollEvery = time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	return newRPC(sock)
}

var (
	running = fakeState{20, "RUNNING", 0}
	exited0 = fakeState{100, "EXITED", 0}
)

func TestRunOnceWaitsForTheEndAndItsStatus(t *testing.T) {
	f := &fakeSupervisord{states: []fakeState{running, running, exited0}}
	if err := serveFake(t, f).runOnce("postfix-reload"); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if f.starts != 1 || f.asked != 3 {
		t.Fatalf("starts %d, asked %d; want 1 start and asked until EXITED (3)", f.starts, f.asked)
	}
}

func TestRunOnceReportsAFailedRun(t *testing.T) {
	f := &fakeSupervisord{states: []fakeState{running, {100, "EXITED", 1}}}
	err := serveFake(t, f).runOnce("postfix-reload")
	if err == nil || !strings.Contains(err.Error(), "ended EXITED, exit status 1") {
		t.Fatalf("runOnce: %v; want the exit status 1", err)
	}
}

// A run in progress may have read the files before the caller wrote them:
// it is waited out and a run of the caller's own follows.
func TestRunOnceStartsItsOwnAfterARunInProgress(t *testing.T) {
	f := &fakeSupervisord{alreadyStarted: 1, states: []fakeState{running, exited0, running, exited0}}
	if err := serveFake(t, f).runOnce("postfix-reload"); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if f.starts != 2 || f.asked != 4 {
		t.Fatalf("starts %d, asked %d; want the running one waited out, then a start of its own (2 starts, 4 asks)", f.starts, f.asked)
	}
}

func TestRunOnceGivesUpOnARunThatDoesNotEnd(t *testing.T) {
	f := &fakeSupervisord{states: []fakeState{running}}
	c := serveFake(t, f)
	old := runOnceFor
	runOnceFor = 20 * time.Millisecond
	t.Cleanup(func() { runOnceFor = old })
	err := c.runOnce("postfix-reload")
	if err == nil || !strings.Contains(err.Error(), "still RUNNING") {
		t.Fatalf("runOnce: %v; want still RUNNING", err)
	}
}

func TestFaultIsReadFromTheAnswer(t *testing.T) {
	f := &fakeSupervisord{alreadyStarted: 1, states: []fakeState{exited0}}
	_, err := serveFake(t, f).call("supervisor.startProcess", "postfix-reload", false)
	fault, ok := err.(*Fault)
	if !ok || fault.Code != faultAlreadyStarted || fault.String != "ALREADY_STARTED: postfix-reload" {
		t.Fatalf("call: %v; want fault 60 ALREADY_STARTED", err)
	}
}
