package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HashToken returns the SHA-256 of raw token bytes. The database stores only this.
func HashToken(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

// NewToken returns 32 cryptographically random bytes. Encode them to show the
// caller once; persist only HashToken of the same bytes.
func NewToken() ([]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	return raw, nil
}

// EncodeToken is the wire form printed once at mint.
func EncodeToken(raw []byte) string {
	return hex.EncodeToString(raw)
}

// DecodeToken parses the wire form. It rejects anything that is not 32 bytes.
func DecodeToken(s string) ([]byte, error) {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("token: want 32 bytes, got %d", len(raw))
	}
	return raw, nil
}
