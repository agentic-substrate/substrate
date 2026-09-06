package identity

import (
	"crypto/sha256"
	"testing"
)

func TestHashTokenIsSHA256OfRawBytes(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	got := HashToken(raw)
	want := sha256.Sum256(raw)
	if len(got) != 32 {
		t.Fatalf("hash length %d, want 32", len(got))
	}
	if string(got) != string(want[:]) {
		t.Fatalf("HashToken is not SHA-256 of the raw 32 bytes")
	}
}

func TestNewTokenIs32RandomBytes(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("len %d and %d, want 32", len(a), len(b))
	}
	if string(a) == string(b) {
		t.Fatal("NewToken returned the same bytes twice")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	raw, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	s := EncodeToken(raw)
	got, err := DecodeToken(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatal("round trip lost bytes")
	}
	if _, err := DecodeToken("not-hex"); err == nil {
		t.Fatal("DecodeToken accepted non-hex")
	}
	if _, err := DecodeToken("abcd"); err == nil {
		t.Fatal("DecodeToken accepted a short token")
	}
}
