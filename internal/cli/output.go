package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// printer emits a command's result either as text or, under --json, as the
// concrete value marshalled directly. There is deliberately no shared report
// interface: nothing ranges over these types.
type printer struct {
	w    io.Writer
	json bool
}

// emit writes v as JSON under --json, otherwise calls human.
func (p printer) emit(v any, human func(io.Writer) error) error {
	if !p.json {
		return human(p.w)
	}
	enc := json.NewEncoder(p.w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
