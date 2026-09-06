package compiler

import (
	"math"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func cl100kCount(text string) int {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		panic(err)
	}
	ids, _, err := enc.Encode(text)
	if err != nil {
		panic(err)
	}
	return len(ids)
}

func TestEstimateEmptyIsZero(t *testing.T) {
	if got := Estimate(""); got != 0 {
		t.Fatalf("Estimate(\"\") = %d, want 0", got)
	}
}

func TestEstimateUsesDocumentedFormula(t *testing.T) {
	text := "hello world — a short pack fragment with mixed punctuation 123"
	got := Estimate(text)
	cl100k := cl100kCount(text)
	bytesThird := float64(len(text)) / 3
	want := int(math.Ceil(math.Max(float64(cl100k), bytesThird) * 1.15))
	if got != want {
		t.Fatalf("Estimate = %d, want max(cl100k=%d, bytes/3=%.3f)*1.15 = %d (EDD §5.2)", got, cl100k, bytesThird, want)
	}
	if got < int(math.Ceil(bytesThird*1.15)) {
		t.Fatalf("Estimate %d is below bytes/3 * 1.15; the estimator is not conservative", got)
	}
}

func TestEstimateIsAtLeastTiktokenHeadroom(t *testing.T) {
	text := "instruction python.version=3.12 preference editor=vim"
	got := Estimate(text)
	tok := cl100kCount(text)
	floor := int(math.Ceil(float64(tok) * 1.15))
	if got < floor {
		t.Fatalf("Estimate(%q) = %d, want >= cl100k*1.15 = %d", text, got, floor)
	}
}
