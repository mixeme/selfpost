// Package secretfile wraps SelfPost's secret-bearing downloads — the full
// server backup and the single-domain export — in a password-encrypted
// envelope. Both artefacts carry DKIM private keys, SASL credentials and
// application passwords in the clear, so an operator who stores them outside
// the server (the whole point of a backup) has to protect them by hand. The
// envelope makes that optional-but-easy: tick a box, give a password, and the
// file that leaves the panel is useless without it.
//
// Layout — a fixed header followed by a sequence of independently
// authenticated chunks, so both writing and reading stream and a multi-megabyte
// backup never has to sit in memory:
//
//	magic      9  "SELFPOST1"  (the trailing digit is the envelope version)
//	type       1  PayloadType: what the plaintext is
//	kdf        1  KDF identifier (1 = scrypt)
//	logN       1  scrypt cost, log2(N)
//	r          4  scrypt block size      (big-endian)
//	p          4  scrypt parallelisation (big-endian)
//	salt      16  scrypt salt
//	prefix     8  nonce prefix
//	chunks     …  repeated: length (4, big-endian) + AES-256-GCM ciphertext
//
// The KDF parameters travel in the header so a file encrypted today still opens
// after the cost is raised. Every chunk is sealed with the nonce prefix plus its
// own counter, and takes the whole header, that counter and an end-of-stream
// flag as additional data: a chunk cannot be reordered, swapped between files,
// or dropped from the end without decryption failing.
package secretfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/scrypt"
)

// PayloadType identifies what a decrypted envelope contains, so opening a file
// can report "that is a domain export, not a backup" instead of handing an
// unexpected payload to a parser.
type PayloadType byte

const (
	// TypeFullBackup is the gzip-compressed tar of a full server backup.
	TypeFullBackup PayloadType = 1
	// TypeDomainExport is the JSON document of a single-domain export.
	TypeDomainExport PayloadType = 2
)

// String names a payload type for operator-facing messages.
func (t PayloadType) String() string {
	switch t {
	case TypeFullBackup:
		return "full backup"
	case TypeDomainExport:
		return "domain export"
	default:
		return fmt.Sprintf("unknown type %d", byte(t))
	}
}

// File name suffixes for the two encrypted artefacts: SelfPost BacKup and
// SelfPost Domain Export. They exist so an operator can tell an encrypted file
// from a plain .tar.gz/.json at a glance; nothing reads them back — the magic
// bytes decide.
const (
	ExtBackup       = ".spbk"
	ExtDomainExport = ".spde"
)

// magic prefixes every envelope. MagicLen bytes are enough to tell an encrypted
// file from a plain one, which is what an import handler needs to decide whether
// to ask for a password.
var magic = []byte("SELFPOST1")

// MagicLen is the number of leading bytes HasMagic inspects.
const MagicLen = 9

const (
	kdfScrypt = 1

	saltLen        = 16
	noncePrefixLen = 8
	counterLen     = 4
	headerLen      = MagicLen + 1 + 1 + 1 + 4 + 4 + saltLen + noncePrefixLen

	// chunkSize is the plaintext carried by one sealed chunk. 64 KiB keeps the
	// per-chunk overhead negligible while bounding the buffer a reader must
	// allocate for a hostile length field.
	chunkSize = 64 * 1024
	tagLen    = 16
	keyLen    = 32 // AES-256
)

// Default scrypt parameters: 32 MiB and roughly a tenth of a second per attempt
// on ordinary hardware. The panel derives a key at most once per download, so
// the cost is invisible to the operator and expensive for anyone brute-forcing a
// stolen backup.
const (
	defaultLogN = 15
	defaultR    = 8
	defaultP    = 1

	// maxLogN caps what a file may ask for, so a hostile header cannot make the
	// reader allocate its way to an out-of-memory kill before it ever fails
	// authentication.
	maxLogN = 20
	maxR    = 32
	maxP    = 16
)

var (
	// ErrNotEncrypted reports a file that is not a SelfPost envelope at all —
	// most often a plain .json export handed to the encrypted path.
	ErrNotEncrypted = errors.New("secretfile: not an encrypted SelfPost file")
	// ErrWrongPassword reports a failed authentication: the password is wrong,
	// or the file has been altered. The two are indistinguishable by design.
	ErrWrongPassword = errors.New("secretfile: wrong password or corrupted file")
	// ErrCorrupt reports structural damage — a truncated or malformed envelope.
	ErrCorrupt = errors.New("secretfile: corrupted file")
)

// HasMagic reports whether b begins with the envelope magic. b may be shorter
// than MagicLen (a short file simply is not an envelope).
func HasMagic(b []byte) bool {
	if len(b) < MagicLen {
		return false
	}
	for i, c := range magic {
		if b[i] != c {
			return false
		}
	}
	return true
}

// header is the parsed fixed prefix, kept alongside its raw bytes because those
// bytes are the additional data every chunk is authenticated with.
type header struct {
	raw         []byte
	typ         PayloadType
	logN        uint8
	r, p        uint32
	salt        []byte
	noncePrefix []byte
}

func (h *header) marshal() []byte {
	buf := make([]byte, 0, headerLen)
	buf = append(buf, magic...)
	buf = append(buf, byte(h.typ), kdfScrypt, h.logN)
	buf = binary.BigEndian.AppendUint32(buf, h.r)
	buf = binary.BigEndian.AppendUint32(buf, h.p)
	buf = append(buf, h.salt...)
	buf = append(buf, h.noncePrefix...)
	return buf
}

func parseHeader(buf []byte) (*header, error) {
	if !HasMagic(buf) {
		return nil, ErrNotEncrypted
	}
	if len(buf) != headerLen {
		return nil, ErrCorrupt
	}
	h := &header{raw: buf}
	i := MagicLen
	h.typ = PayloadType(buf[i])
	if buf[i+1] != kdfScrypt {
		return nil, fmt.Errorf("%w: unsupported key derivation %d", ErrCorrupt, buf[i+1])
	}
	h.logN = buf[i+2]
	h.r = binary.BigEndian.Uint32(buf[i+3 : i+7])
	h.p = binary.BigEndian.Uint32(buf[i+7 : i+11])
	i += 11
	h.salt = buf[i : i+saltLen]
	h.noncePrefix = buf[i+saltLen : i+saltLen+noncePrefixLen]

	// Reject absurd work factors before spending any memory on them.
	if h.logN < 1 || h.logN > maxLogN || h.r < 1 || h.r > maxR || h.p < 1 || h.p > maxP {
		return nil, fmt.Errorf("%w: unreasonable key-derivation parameters", ErrCorrupt)
	}
	return h, nil
}

// deriveKey runs scrypt with the header's parameters.
func (h *header) deriveKey(password string) ([]byte, error) {
	key, err := scrypt.Key([]byte(password), h.salt, 1<<h.logN, int(h.r), int(h.p), keyLen)
	if err != nil {
		return nil, fmt.Errorf("secretfile: derive key: %w", err)
	}
	return key, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretfile: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretfile: gcm: %w", err)
	}
	return aead, nil
}

// nonce composes the per-chunk nonce: the file's random prefix followed by the
// chunk counter, so no two chunks in a file — and, with overwhelming
// probability, no two chunks across files — share one.
func nonce(prefix []byte, counter uint32) []byte {
	n := make([]byte, 0, noncePrefixLen+counterLen)
	n = append(n, prefix...)
	return binary.BigEndian.AppendUint32(n, counter)
}

// chunkAAD binds a chunk to its file, its position and its end-of-stream flag.
func chunkAAD(hdr []byte, counter uint32, last bool) []byte {
	aad := make([]byte, 0, len(hdr)+counterLen+1)
	aad = append(aad, hdr...)
	aad = binary.BigEndian.AppendUint32(aad, counter)
	if last {
		return append(aad, 1)
	}
	return append(aad, 0)
}

// Writer encrypts a stream into w. Callers must Close it: the final chunk (and
// with it the end-of-stream marker that makes truncation detectable) is only
// written then.
type Writer struct {
	w       io.Writer
	aead    cipher.AEAD
	hdr     *header
	buf     []byte // pending plaintext, at most chunkSize
	sealed  []byte // reusable ciphertext scratch
	counter uint32
	closed  bool
	err     error
}

// NewWriter derives a key from password and writes the envelope header to w.
// Deriving the key is deliberately slow (scrypt), so call this once per file.
func NewWriter(w io.Writer, typ PayloadType, password string) (*Writer, error) {
	if password == "" {
		return nil, errors.New("secretfile: empty password")
	}
	h := &header{
		typ:         typ,
		logN:        defaultLogN,
		r:           defaultR,
		p:           defaultP,
		salt:        make([]byte, saltLen),
		noncePrefix: make([]byte, noncePrefixLen),
	}
	if _, err := rand.Read(h.salt); err != nil {
		return nil, fmt.Errorf("secretfile: salt: %w", err)
	}
	if _, err := rand.Read(h.noncePrefix); err != nil {
		return nil, fmt.Errorf("secretfile: nonce: %w", err)
	}
	h.raw = h.marshal()

	key, err := h.deriveKey(password)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(h.raw); err != nil {
		return nil, fmt.Errorf("secretfile: write header: %w", err)
	}
	return &Writer{
		w:      w,
		aead:   aead,
		hdr:    h,
		buf:    make([]byte, 0, chunkSize),
		sealed: make([]byte, 0, chunkSize+tagLen),
	}, nil
}

// Write buffers plaintext, sealing and emitting a chunk whenever a full one has
// accumulated.
func (e *Writer) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	if e.closed {
		return 0, errors.New("secretfile: write after close")
	}
	written := 0
	for len(p) > 0 {
		n := chunkSize - len(e.buf)
		if n > len(p) {
			n = len(p)
		}
		e.buf = append(e.buf, p[:n]...)
		p = p[n:]
		written += n
		if len(e.buf) == chunkSize {
			if err := e.flush(false); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

// flush seals the buffered plaintext as one chunk. last marks the end of the
// stream inside the authenticated data.
func (e *Writer) flush(last bool) error {
	e.sealed = e.aead.Seal(e.sealed[:0], nonce(e.hdr.noncePrefix, e.counter), e.buf, chunkAAD(e.hdr.raw, e.counter, last))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(e.sealed)))
	if _, err := e.w.Write(length[:]); err != nil {
		e.err = fmt.Errorf("secretfile: write chunk: %w", err)
		return e.err
	}
	if _, err := e.w.Write(e.sealed); err != nil {
		e.err = fmt.Errorf("secretfile: write chunk: %w", err)
		return e.err
	}
	e.buf = e.buf[:0]
	e.counter++
	return nil
}

// Close seals whatever is buffered as the final chunk. It does not close the
// underlying writer.
func (e *Writer) Close() error {
	if e.err != nil {
		return e.err
	}
	if e.closed {
		return nil
	}
	e.closed = true
	// Always emitted, even for empty input: the end-of-stream chunk is what
	// proves the file was not truncated.
	return e.flush(true)
}

// Reader decrypts an envelope. Chunks are verified as they are read, so a
// truncated or tampered file surfaces as a read error rather than as short but
// plausible plaintext.
type Reader struct {
	r       io.Reader
	aead    cipher.AEAD
	hdr     *header
	plain   []byte // decrypted, not yet handed to the caller
	sealed  []byte // reusable ciphertext scratch
	counter uint32
	done    bool
	err     error
}

// NewReader reads and verifies the envelope header, derives the key and returns
// a Reader over the plaintext. It fails with ErrNotEncrypted for a file that is
// not an envelope; a wrong password is only detected on the first Read, when
// the first chunk fails authentication.
func NewReader(r io.Reader, password string) (*Reader, error) {
	raw := make([]byte, headerLen)
	if _, err := io.ReadFull(r, raw); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			// Too short to be an envelope: if what we did read is not even the
			// magic, say so — that is the common "plain file" case.
			if !HasMagic(raw) {
				return nil, ErrNotEncrypted
			}
			return nil, ErrCorrupt
		}
		return nil, fmt.Errorf("secretfile: read header: %w", err)
	}
	h, err := parseHeader(raw)
	if err != nil {
		return nil, err
	}
	key, err := h.deriveKey(password)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Reader{r: r, aead: aead, hdr: h}, nil
}

// Type reports what the envelope claims to contain. The claim is authenticated
// (the header is additional data for every chunk), so it is trustworthy as soon
// as the first Read succeeds.
func (d *Reader) Type() PayloadType { return d.hdr.typ }

// Read returns decrypted plaintext, verifying one chunk at a time.
func (d *Reader) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			d.err = err
			return 0, err
		}
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

// next reads, verifies and decrypts the following chunk, setting done when the
// chunk carries the end-of-stream flag.
func (d *Reader) next() error {
	var length [4]byte
	if _, err := io.ReadFull(d.r, length[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			// The stream ended without a chunk marked last: truncated.
			return ErrCorrupt
		}
		return fmt.Errorf("secretfile: read chunk: %w", err)
	}
	n := binary.BigEndian.Uint32(length[:])
	if n < tagLen || n > chunkSize+tagLen {
		return ErrCorrupt
	}
	if cap(d.sealed) < int(n) {
		d.sealed = make([]byte, n)
	}
	d.sealed = d.sealed[:n]
	if _, err := io.ReadFull(d.r, d.sealed); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrCorrupt
		}
		return fmt.Errorf("secretfile: read chunk: %w", err)
	}

	// A chunk authenticates under exactly one of the two end-of-stream flags;
	// trying the ordinary one first keeps the common case to a single open.
	nc := nonce(d.hdr.noncePrefix, d.counter)
	plain, err := d.aead.Open(nil, nc, d.sealed, chunkAAD(d.hdr.raw, d.counter, false))
	if err != nil {
		plain, err = d.aead.Open(nil, nc, d.sealed, chunkAAD(d.hdr.raw, d.counter, true))
		if err != nil {
			return ErrWrongPassword
		}
		d.done = true
	}
	d.plain = plain
	d.counter++
	return nil
}
