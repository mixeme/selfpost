package milter

import (
	"context"
	"net"
	"net/textproto"
	"sync"
	"testing"

	"github.com/emersion/go-milter"
)

// headerRecorder is a backend that notes what the library hands it.
type headerRecorder struct {
	milter.NoOpMilter
	mu       *sync.Mutex
	headers  *[]string
	mapSizes *[]int
}

func (r headerRecorder) Header(name, value string, m *milter.Modifier) (milter.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.headers = append(*r.headers, name)
	return milter.RespContinue, nil
}

func (r headerRecorder) Headers(h textproto.MIMEHeader, m *milter.Modifier) (milter.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.mapSizes = append(*r.mapSizes, len(h))
	return milter.RespContinue, nil
}

// The go-milter the panel builds (third_party/go-milter/PATCHES.md) hands
// every header to Header and keeps none: the map Headers gets is empty, on
// the first message of a connection and on the next - upstream kept every
// header of the connection until an ABORT.
func TestTheLibraryKeepsNoHeaders(t *testing.T) {
	var mu sync.Mutex
	var headers []string
	var mapSizes []int
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &milter.Server{
		NewMilter: func() milter.Milter { return headerRecorder{mu: &mu, headers: &headers, mapSizes: &mapSizes} },
		Protocol:  milter.OptNoBody,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-ctx.Done(); _ = srv.Close() }()
	go func() { _ = srv.Serve(ln) }()

	cl := milter.NewClientWithOptions("tcp", ln.Addr().String(), milter.ClientOptions{})
	defer cl.Close()
	s, err := cl.Session()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 2; i++ {
		if _, err := s.Mail("<a@example.org>", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Rcpt("<b@example.net>", nil); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"From", "To", "X-Filler"} {
			if _, err := s.HeaderField(name, " value"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.HeaderEnd(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(headers) != 6 {
		t.Errorf("Header saw %v, want the three headers of each of two messages", headers)
	}
	if len(mapSizes) != 2 || mapSizes[0] != 0 || mapSizes[1] != 0 {
		t.Errorf("Headers got maps of %v headers, want two empty ones", mapSizes)
	}
}
