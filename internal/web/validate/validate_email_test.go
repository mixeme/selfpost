package validate

import "testing"

func TestValidateEmail(t *testing.T) {
	if err := Email(""); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := Email("reports@mail.example.com"); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := Email("bad"); err == nil {
		t.Error("bad address accepted")
	}
	if err := Email("x@gmail.com"); err == nil {
		t.Error("gmail accepted")
	}
}
