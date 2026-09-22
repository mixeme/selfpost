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
	if err := Email("reports+rua@mail.example.com"); err != nil {
		t.Errorf("plus-address valid: %v", err)
	}
	if err := Email("bad space@mail.example.com"); err == nil {
		t.Error("address with space accepted")
	}
	if err := Email("привет@mail.example.com"); err == nil {
		t.Error("non-ascii local-part accepted")
	}
	if err := Email("x@-example.com"); err == nil {
		t.Error("invalid domain accepted")
	}
	if err := Email("x@protonmail.com"); err == nil {
		t.Error("blocked freemail domain accepted")
	}
}
