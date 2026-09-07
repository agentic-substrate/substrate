package importer

import (
	"sort"
	"strings"
)

// BuildPlan classifies inventory blocks after deduping them by content hash.
// Passing a non-nil Classifier is how tests prove that order: classify is
// invoked once per unique hash, not once per source block.
func BuildPlan(invs []Inventory, classify Classifier) (*Plan, error) {
	if classify == nil {
		classify = Classify
	}

	type raw struct {
		hash, heading, body, scope, detected, rel string
		source                                    Source
	}
	var all []raw
	for _, inv := range invs {
		for _, f := range inv.Files {
			if f.DetectedType == "memorix" {
				continue
			}
			for _, blk := range splitBlocks(f.Content) {
				body := strings.TrimSpace(blk.body)
				if body == "" {
					continue
				}
				all = append(all, raw{
					hash:     sha256Hex([]byte(body)),
					heading:  blk.heading,
					body:     body,
					scope:    f.ImpliedScope,
					detected: f.DetectedType,
					rel:      f.Rel,
					source:   Source{Hostname: inv.Hostname, Path: f.Path, Rel: f.Rel},
				})
			}
		}
	}

	byHash := make(map[string]*Block, len(all))
	order := make([]string, 0, len(all))
	for _, r := range all {
		if existing, ok := byHash[r.hash]; ok {
			existing.Sources = append(existing.Sources, r.source)
			continue
		}
		byHash[r.hash] = &Block{
			Hash:         r.hash,
			Heading:      r.heading,
			Body:         r.body,
			ImpliedScope: r.scope,
			DetectedType: r.detected,
			Rel:          r.rel,
			Sources:      []Source{r.source},
		}
		order = append(order, r.hash)
	}

	for _, h := range order {
		c := classify(*byHash[h])
		byHash[h].Kind = c.Kind
		byHash[h].Confidence = c.Confidence
		byHash[h].Note = c.Note
	}

	type slotKey struct{ rel, heading string }
	bySlot := make(map[slotKey][]*Block)
	slotOrder := make([]slotKey, 0)
	seenSlot := make(map[slotKey]struct{})
	for _, h := range order {
		b := byHash[h]
		k := slotKey{rel: b.Rel, heading: b.Heading}
		if _, ok := seenSlot[k]; !ok {
			seenSlot[k] = struct{}{}
			slotOrder = append(slotOrder, k)
		}
		bySlot[k] = append(bySlot[k], b)
	}

	plan := &Plan{}
	for _, k := range slotOrder {
		blocks := bySlot[k]
		if len(blocks) == 1 || !slotHasDistinctOrigins(blocks) {
			for _, b := range blocks {
				plan.Blocks = append(plan.Blocks, *b)
			}
			continue
		}
		c := Conflict{Slot: k.rel + "#" + k.heading}
		for _, b := range blocks {
			c.Pair = append(c.Pair, ConflictSide{
				Hash:      b.Hash,
				Hostnames: hostnames(b.Sources),
				Body:      b.Body,
			})
		}
		plan.Conflicts = append(plan.Conflicts, c)
	}
	sort.Slice(plan.Blocks, func(i, j int) bool { return plan.Blocks[i].Hash < plan.Blocks[j].Hash })
	sort.Slice(plan.Conflicts, func(i, j int) bool { return plan.Conflicts[i].Slot < plan.Conflicts[j].Slot })
	return plan, nil
}

type mdBlock struct {
	heading, body string
}

func splitBlocks(content string) []mdBlock {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	var heading string
	var buf []string
	var out []mdBlock
	flush := func() {
		var para []string
		flushPara := func() {
			p := strings.TrimSpace(strings.Join(para, "\n"))
			para = nil
			if p != "" {
				out = append(out, mdBlock{heading: heading, body: p})
			}
		}
		for _, line := range buf {
			trim := strings.TrimSpace(line)
			switch {
			case trim == "":
				flushPara()
			case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
				flushPara()
				text := strings.TrimSpace(trim[2:])
				if text != "" {
					out = append(out, mdBlock{heading: heading, body: text})
				}
			case strings.HasPrefix(trim, "@") || strings.HasPrefix(trim, "<!--"):
				continue
			default:
				para = append(para, trim)
			}
		}
		flushPara()
		buf = nil
	}
	for _, line := range lines {
		if h, ok := markdownHeading(line); ok {
			flush()
			heading = h
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return out
}

func markdownHeading(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	n := 0
	for n < len(trim) && trim[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return "", false
	}
	if n < len(trim) && trim[n] != ' ' && trim[n] != '\t' {
		return "", false
	}
	return strings.TrimSpace(trim[n:]), true
}

func hostnames(sources []Source) []string {
	seen := make(map[string]struct{}, len(sources))
	var out []string
	for _, s := range sources {
		if _, ok := seen[s.Hostname]; ok {
			continue
		}
		seen[s.Hostname] = struct{}{}
		out = append(out, s.Hostname)
	}
	sort.Strings(out)
	return out
}

func slotHasDistinctOrigins(blocks []*Block) bool {
	hosts := make(map[string]struct{})
	paths := make(map[string]struct{})
	for _, b := range blocks {
		for _, s := range b.Sources {
			hosts[s.Hostname] = struct{}{}
			paths[s.Path] = struct{}{}
		}
	}
	return len(hosts) > 1 || len(paths) > 1
}
