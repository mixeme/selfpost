package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/legal"
)

func TestLicenseHandlerServesAGPL(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/license", nil)
	s.handleLicense(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "GNU AFFERO GENERAL PUBLIC LICENSE") {
		t.Error("response is missing the AGPL title")
	}
	if body != string(legal.License) {
		t.Error("response body does not match legal.License")
	}
}

func TestLicenseHandlerRejectsNonGET(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/license", nil)
	s.handleLicense(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
