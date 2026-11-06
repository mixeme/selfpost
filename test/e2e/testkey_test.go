package e2e

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// testRSAKey returns a fixed, public test fixture. It never protects or signs
// real data, and reading it avoids paying for RSA generation in every test.
func testRSAKey() (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rsa-2048.pem"))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("decode fixed test RSA key")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
