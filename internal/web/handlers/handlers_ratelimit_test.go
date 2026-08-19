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
		"max_messages":   {"80"},
		"window_seconds": {"3600"},
	}), 100)
	if err != nil || in.maxMessages != 80 {
		t.Fatalf("valid app limit = %+v err=%v", in, err)
	}

	in, err = parseAppRateLimitForm(form(url.Values{"max_messages": {""}}), 100)
	if err != nil || !in.clear {
		t.Fatalf("empty max should clear: %+v err=%v", in, err)
	}

	_, err = parseAppRateLimitForm(form(url.Values{
		"mode":           {"manual"},
		"max_messages":   {"150"},
		"window_seconds": {"3600"},
	}), 100)
	if err == nil || !strings.Contains(err.Error(), "level-1") {
		t.Fatalf("over L1 want error, got %v", err)
	}
}

func TestParseAppAuthIPsForm(t *testing.T) {
	t.Parallel()
	form := func(vals url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(vals.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}

	restrict, ips, err := parseAppAuthIPsForm(form(url.Values{
		"auth_ip_restrict": {"1"},
		"auth_allowed_ips": {"203.0.113.10"},
	}))
	if err != nil || !restrict || len(ips) != 1 {
		t.Fatalf("enabled with IP = restrict=%v ips=%v err=%v", restrict, ips, err)
	}

	restrict, ips, err = parseAppAuthIPsForm(form(url.Values{}))
	if err != nil || restrict || ips != nil {
		t.Fatalf("disabled = restrict=%v ips=%v err=%v", restrict, ips, err)
	}

	_, _, err = parseAppAuthIPsForm(form(url.Values{"auth_ip_restrict": {"1"}}))
	if err == nil || !strings.Contains(err.Error(), "at least one client IP") {
		t.Fatalf("enabled without IPs want error, got %v", err)
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
		"mode":            {"auto"},
		"auto_multiplier": {"2.5"},
	}), 100)
	if err != nil || in.mode != "auto" || in.autoMultiplier != 2.5 {
		t.Fatalf("auto domain = %+v err=%v", in, err)
	}
	_, err = parseDomainRateLimitForm(form(url.Values{
		"mode":            {"auto"},
		"auto_multiplier": {"10"},
	}), 100)
	if err == nil {
		t.Fatal("multiplier out of range should fail")
	}
}
