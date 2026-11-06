package handlers

import (
	"sync"
	"testing"

	"github.com/mixeme/selfpost/internal/web/view"
)

var sharedView = sync.OnceValues(func() (*view.Engine, error) {
	return view.New("test")
})

func mustView(t *testing.T) *view.Engine {
	t.Helper()
	v, err := sharedView()
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	return v
}

// Inbound is an Engine flag, so its stands share a second immutable
// configuration instead of switching the default engine on for every test.
var sharedInboundView = sync.OnceValues(func() (*view.Engine, error) {
	v, err := view.New("test")
	if err == nil {
		v.SetInboundEnabled(true)
	}
	return v, err
})

func mustInboundView(t *testing.T) *view.Engine {
	t.Helper()
	v, err := sharedInboundView()
	if err != nil {
		t.Fatalf("inbound view: %v", err)
	}
	return v
}
