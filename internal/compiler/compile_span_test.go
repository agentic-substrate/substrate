package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Goes red if database span End is not deferred. TxChecked can panic and
// net/http recovers; without defer the span is never exported. Removing
// the word defer from `defer dbSpan.End()` is the one-line production
// change that makes this fail.
func TestDatabaseSpanEndIsDeferred(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(file), "compile.go")) //nolint:gosec
	if err != nil {
		t.Fatalf("read compile.go: %v", err)
	}
	if !bytes.Contains(src, []byte("defer dbSpan.End()")) {
		t.Fatal("database span End is not deferred; a panic inside TxChecked (which net/http recovers) leaves the span unexported")
	}
}
