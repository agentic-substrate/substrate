package mcpbridge

import (
	"bufio"
	"bytes"
	"errors"
	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/agentic-substrate/substrate/internal/strictjson"
	"io"
)

// Validate before the SDK decoder can normalize invalid text into a different contribution.
type frameReader struct {
	source  io.Reader
	scanner *bufio.Scanner
	pending *bytes.Reader
}

func newFrameReader(r io.Reader) *frameReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), node.MaxFrame)
	return &frameReader{source: r, scanner: scanner}
}
func (f *frameReader) Read(p []byte) (int, error) {
	if f.pending != nil && f.pending.Len() > 0 {
		return f.pending.Read(p)
	}
	if !f.scanner.Scan() {
		if err := f.scanner.Err(); err != nil {
			return 0, err
		}
		return 0, io.EOF
	}
	line := f.scanner.Bytes()
	if !strictjson.ValidText(line) {
		return 0, errors.New("MCP frame requires valid UTF-8 and paired Unicode escapes")
	}
	f.pending = bytes.NewReader(append(append([]byte{}, line...), '\n'))
	return f.pending.Read(p)
}

func (f *frameReader) Close() error {
	if closer, ok := f.source.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
