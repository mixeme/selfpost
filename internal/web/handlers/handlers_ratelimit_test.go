package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseDomainRateLimitForm(t *testing.T) {
	t.Parallel()
	form := func(vals url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(vals.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}

	in, err := parseDomainRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"max_messages":   {"50"},
		"window_seconds": {"3600"},
	}), 100)
	if err != nil || in.clear || in.maxMessages != 50 || in.windowSeconds != 3600 || len(in.ips) != 0 {
		t.Fatalf("valid domain = %+v err=%v", in, err)
	}

	in, err = parseDomainRateLimitForm(form(url.Values{"max_messages": {""}}), 100)
	if err != nil || !in.clear {
		t.Fatalf("empty max should clear: %+v err=%v", in, err)
	}

	_, err = parseDomainRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"max_messages":   {"150"},
		"window_seconds": {"3600"},
	}), 100)
	if err == nil || !strings.Contains(err.Error(), "level-1") {
		t.Fatalf("over L1 want error, got %v", err)
	}
}

func TestParseAppRateLimitForm(t *testing.T) {
	t.Parallel()
	form := func(vals url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(vals.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}

	in, err := parseAppRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"allowed_ips":    {"203.0.113.10"},
		"max_messages":   {"80"},
		"window_seconds": {"3600"},
	}), 100, 40, true)
	if err != nil || in.maxMessages != 80 || len(in.ips) != 1 {
		t.Fatalf("valid app override = %+v err=%v", in, err)
	}

	_, err = parseAppRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"max_messages":   {"80"},
		"window_seconds": {"3600"},
	}), 100, 40, true)
	if err == nil || !strings.Contains(err.Error(), "trusted client IP") {
		t.Fatalf("missing IPs want error, got %v", err)
	}

	_, err = parseAppRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"allowed_ips":    {"203.0.113.10"},
		"max_messages":   {"40"},
		"window_seconds": {"3600"},
	}), 100, 40, true)
	if err == nil || !strings.Contains(err.Error(), "greater than the domain") {
		t.Fatalf("app <= domain want error, got %v", err)
	}

	_, err = parseAppRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"allowed_ips":    {"203.0.113.10"},
		"max_messages":   {"150"},
		"window_seconds": {"3600"},
	}), 100, 0, false)
	if err == nil || !strings.Contains(err.Error(), "level-1") {
		t.Fatalf("over L1 want error, got %v", err)
	}

	// No domain limit: any app ceiling ≤ L1 is fine.
	in, err = parseAppRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"allowed_ips":    {"203.0.113.10"},
		"max_messages":   {"50"},
		"window_seconds": {"3600"},
	}), 100, 0, false)
	if err != nil || in.maxMessages != 50 {
		t.Fatalf("app without domain = %+v err=%v", in, err)
	}
}

func TestParseDomainRateLimitFormAuto(t *testing.T) {
	t.Parallel()
	form := func(vals url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(vals.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	in, err := parseDomainRateLimitForm(form(url.Values{
		"mode":             {"auto"},
		"auto_multiplier":  {"2.5"},
	}), 100)
	if err != nil || in.mode != "auto" || in.autoMultiplier != 2.5 {
		t.Fatalf("auto domain = %+v err=%v", in, err)
	}
	_, err = parseDomainRateLimitForm(form(url.Values{
		"mode":             {"auto"},
		"auto_multiplier":  {"10"},
	}), 100)
	if err == nil {
		t.Fatal("multiplier out of range should fail")
	}
}
