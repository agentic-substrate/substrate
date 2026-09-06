package compiler

import (
	"math"
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

var (
	encOnce sync.Once
	enc     tokenizer.Codec
	encErr  error
)

// Estimate is the documented conservative token estimator (EDD §5.2, R22):
//
//	estimate = max(tiktoken_cl100k(text), bytes/3) * 1.15
//
// Acceptance is stated against this function, not a harness tokenizer.
func Estimate(text string) int {
	if text == "" {
		return 0
	}
	tok := cl100kTokens(text)
	bytesThird := float64(len(text)) / 3
	return int(math.Ceil(math.Max(float64(tok), bytesThird) * 1.15))
}

func cl100kTokens(text string) int {
	encOnce.Do(func() {
		enc, encErr = tokenizer.Get(tokenizer.Cl100kBase)
	})
	if encErr != nil || enc == nil {
		return len(text)
	}
	ids, _, err := enc.Encode(text)
	if err != nil {
		return len(text)
	}
	return len(ids)
}
