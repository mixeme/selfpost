package secretfile

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// encrypt is the round-trip helper: seal data and return the envelope bytes.
func encrypt(t *testing.T, typ PayloadType, password string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, typ, password)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

func decrypt(t *testing.T, enc []byte, password string) ([]byte, PayloadType, error) {
	t.Helper()
	r, err := NewReader(bytes.NewReader(enc), password)
	if err != nil {
		return nil, 0, err
	}
	out, err := io.ReadAll(r)
	return out, r.Type(), err
}

func TestRoundTripSizes(t *testing.T) {
	// Empty, sub-chunk, exactly one chunk, and several chunks with a partial
	// tail: the boundaries where chunk framing tends to break.
	sizes := []int{0, 1, 1000, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 77}
	for _, n := range sizes {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i * 7)
		}
		enc := encrypt(t, TypeFullBackup, "correct horse battery staple", data)
		got, typ, err := decrypt(t, enc, "correct horse battery staple")
		if err != nil {
			t.Fatalf("size %d: decrypt: %v", n, err)
		}
		if typ != TypeFullBackup {
			t.Errorf("size %d: type = %v, want full backup", n, typ)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("size %d: plaintext mismatch (%d bytes back)", n, len(got))
		}
	}
}

func TestCiphertextDoesNotLeakPlaintext(t *testing.T) {
	secret := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n")
	enc := encrypt(t, TypeDomainExport, "a very long password", secret)
	if bytes.Contains(enc, secret) {
		t.Fatal("plaintext found verbatim in the envelope")
	}
	if !HasMagic(enc) {
		t.Fatal("envelope does not start with the magic")
	}
}

func TestWrongPassword(t *testing.T) {
	enc := encrypt(t, TypeDomainExport, "the right password", []byte("secret payload"))
	_, _, err := decrypt(t, enc, "the wrong password")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
}

func TestNotEncrypted(t *testing.T) {
	for _, plain := range [][]byte{[]byte(`{"format":"selfpost-domain-export"}`), {}, []byte("SELF")} {
		if _, err := NewReader(bytes.NewReader(plain), "pw"); !errors.Is(err, ErrNotEncrypted) {
			t.Errorf("NewReader(%q) = %v, want ErrNotEncrypted", plain, err)
		}
		if HasMagic(plain) {
			t.Errorf("HasMagic(%q) = true", plain)
		}
	}
}

func TestTruncationDetected(t *testing.T) {
	// A backup cut short mid-transfer must fail loudly rather than restore a
	// plausible-looking prefix.
	data := bytes.Repeat([]byte("payload"), 20000) // spans several chunks
	enc := encrypt(t, TypeFullBackup, "password password", data)

	// Drop the final chunk entirely: what remains is a sequence of valid,
	// correctly authenticated chunks, none of which is marked last.
	var chunks []int
	for off := headerLen; off < len(enc); {
		n := int(enc[off])<<24 | int(enc[off+1])<<16 | int(enc[off+2])<<8 | int(enc[off+3])
		chunks = append(chunks, off)
		off += 4 + n
	}
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	truncated := enc[:chunks[len(chunks)-1]]
	r, err := NewReader(bytes.NewReader(truncated), "password password")
	if err != nil {
		t.Fatalf("NewReader (truncated): %v", err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("read truncated = %v, want ErrCorrupt", err)
	}
}

func TestTamperDetected(t *testing.T) {
	enc := encrypt(t, TypeFullBackup, "password password", []byte("some archive bytes"))
	for _, off := range []int{MagicLen /* type byte */, headerLen + 6 /* ciphertext */} {
		bad := bytes.Clone(enc)
		bad[off] ^= 0xff
		if _, _, err := decrypt(t, bad, "password password"); err == nil {
			t.Errorf("flipping byte %d was accepted", off)
		}
	}
}

func TestChunkReorderRejected(t *testing.T) {
	// Two full chunks plus a tail, so swapping the first two is possible without
	// changing any length.
	data := make([]byte, 2*chunkSize+10)
	for i := range data {
		data[i] = byte(i)
	}
	enc := encrypt(t, TypeFullBackup, "password password", data)

	const framed = 4 + chunkSize + tagLen
	first := headerLen
	second := headerLen + framed
	swapped := bytes.Clone(enc)
	copy(swapped[first:first+framed], enc[second:second+framed])
	copy(swapped[second:second+framed], enc[first:first+framed])

	if _, _, err := decrypt(t, swapped, "password password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("reordered chunks = %v, want authentication failure", err)
	}
}

func TestEmptyPasswordRejected(t *testing.T) {
	if _, err := NewWriter(io.Discard, TypeFullBackup, ""); err == nil {
		t.Fatal("empty password accepted")
	}
}

func TestUnreasonableKDFParamsRejected(t *testing.T) {
	enc := encrypt(t, TypeFullBackup, "password password", []byte("x"))
	bad := bytes.Clone(enc)
	bad[MagicLen+2] = 40 // logN far beyond maxLogN
	if _, err := NewReader(bytes.NewReader(bad), "password password"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}
