package handlers

import (
	"testing"

	"github.com/mixeme/selfpost/internal/web/view"
)

func mustView(t *testing.T) *view.Engine {
	t.Helper()
	v, err := view.New("test")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	return v
}
