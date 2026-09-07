package importer

import (
	"regexp"
	"strings"
)

const misfileNote = "heuristic will misfile (R15); imported rows land as proposed"

var (
	styleAllow = []string{
		"indent",
		"indentation",
		"verbosity",
		"verbose",
		"tabs",
		"spaces",
		"comment language",
	}
	firstPerson = regexp.MustCompile(`(?i)\b(i|me|my)\b`)
)

// Classify is an honest heuristic. Confidence is never 1: the kind may be
// wrong and everything imported still lands as proposed (R15).
func Classify(b Block) Classification {
	blob := strings.ToLower(b.Heading + " " + b.Body)
	for _, key := range styleAllow {
		if strings.Contains(blob, key) {
			return Classification{Kind: "preference", Confidence: 0.55, Note: misfileNote}
		}
	}
	if b.ImpliedScope == "user" && firstPerson.MatchString(b.Body) {
		return Classification{Kind: "preference", Confidence: 0.5, Note: misfileNote}
	}
	return Classification{Kind: "instruction", Confidence: 0.35, Note: misfileNote}
}
