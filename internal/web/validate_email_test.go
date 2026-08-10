package web

import "testing"

func TestValidateEmail(t *testing.T) {
	if err := validateEmail(""); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := validateEmail("reports@mail.example.com"); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := validateEmail("bad"); err == nil {
		t.Error("bad address accepted")
	}
	if err := validateEmail("x@gmail.com"); err == nil {
		t.Error("gmail accepted")
	}
}
