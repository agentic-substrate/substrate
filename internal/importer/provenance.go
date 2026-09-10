package importer

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// InventoryWitness is what the scanner observed, reduced to the identities an
// apply needs in order to answer one question: was this block on disk?
//
// It is derived from inventory.json by whoever runs `import apply`, never from
// plan.json. That separation is the whole point (#95): the plan is an operator
// artifact anyone who can write the file can edit, while the witness is
// recomputed from the scan product by the same code that built the plan. A
// block absent from the witness never reached the store, so it can never be
// rendered to an agent as a directive.
//
// The witness carries hashes and locations, not bodies: it is small enough to
// travel beside a plan in one request, and it discloses nothing the plan does
// not already carry.
type InventoryWitness struct {
	Digest   string      `json:"digest"`
	Blocks   []BlockRef  `json:"blocks"`
	Memories []MemoryRef `json:"memories,omitempty"`
}

// BlockRef is one admissible block identity: where the scanner saw it and what
// it said. Kind is deliberately absent — classification is a heuristic an
// operator is allowed to correct (#80), and correcting it cannot invent text.
type BlockRef struct {
	Rel     string `json:"rel"`
	Heading string `json:"heading"`
	Ordinal int    `json:"ordinal"`
	Hash    string `json:"hash"`
}

// MemoryRef is one admissible memory identity from a memorix export.
type MemoryRef struct {
	Hostname string `json:"hostname"`
	Hash     string `json:"hash"`
}

// DigestInventories hashes the inventories a plan was built from: hostname,
// roots, and every file's rel and content hash. Two scans of the same disk
// state produce the same digest; a scan of anything else does not.
func DigestInventories(invs []Inventory) string {
	lines := make([]string, 0, len(invs))
	for _, inv := range invs {
		var b strings.Builder
		b.WriteString("host\x00" + inv.Hostname + "\n")
		roots := append([]string(nil), inv.Roots...)
		sort.Strings(roots)
		for _, r := range roots {
			b.WriteString("root\x00" + r + "\n")
		}
		files := make([]string, 0, len(inv.Files))
		for _, f := range inv.Files {
			files = append(files, "file\x00"+f.Rel+"\x00"+f.Hash+"\n")
		}
		sort.Strings(files)
		for _, f := range files {
			b.WriteString(f)
		}
		lines = append(lines, b.String())
	}
	sort.Strings(lines)
	return sha256Hex([]byte(strings.Join(lines, "\x00")))
}

// WitnessInventories reduces inventories to the identities apply will admit.
// It reuses deriveBlocks, so the witness is produced by the same derivation
// that produces a plan: a second implementation would drift and start refusing
// files a real scan saw.
func WitnessInventories(invs []Inventory) (InventoryWitness, error) {
	slotOrder, bySlot, err := deriveBlocks(invs, nil)
	if err != nil {
		return InventoryWitness{}, err
	}
	w := InventoryWitness{Digest: DigestInventories(invs)}
	for _, k := range slotOrder {
		for _, b := range bySlot[k] {
			w.Blocks = append(w.Blocks, BlockRef{Rel: b.Rel, Heading: b.Heading, Ordinal: b.Ordinal, Hash: b.Hash})
		}
	}
	for _, m := range extractMemories(invs) {
		w.Memories = append(w.Memories, MemoryRef{Hostname: m.Hostname, Hash: m.Hash})
	}
	return w, nil
}

func blockRefKey(rel, heading string, ordinal int, hash string) string {
	return strings.Join([]string{rel, heading, strconv.Itoa(ordinal), hash}, "\x00")
}

// ErrPlanNotWitnessed is what every refusal in CheckPlanWitness wraps. It is a
// caller mistake — a plan that does not match the inventory it names — so the
// REST handler answers 400 rather than letting it fall through as a 500 that
// reports a client error as a server fault and spends the 5xx error budget.
var ErrPlanNotWitnessed = errors.New("the plan does not match the inventory it was built from")

// CheckPlanWitness refuses any plan the scan does not account for.
//
// It answers one question per item: did the scanner see this exact body at this
// exact place? Identity is (rel, heading, ordinal, hash), the same tuple that
// keys the row downstream, and the body is re-hashed rather than trusted, so a
// witnessed identity cannot be reused to carry different text.
//
// What this does NOT claim: the witness is not a signature, and an attacker who
// can rewrite inventory.json, the scanned files themselves, or who calls
// POST /v1/import directly can still supply a matching pair. The binding moves
// the trust from "a file named plan.json" to "what the scan recorded", which is
// what makes the operator's supply of a plan mean the scanner saw it on disk.
func CheckPlanWitness(plan Plan, w InventoryWitness) error {
	if len(plan.Blocks) == 0 && len(plan.Conflicts) == 0 && len(plan.Memories) == 0 {
		// Nothing to admit, so there is nothing an inventory could disagree
		// with. Refusing here would only turn a no-op apply into an error.
		return nil
	}
	if w.Digest == "" {
		return fmt.Errorf("import apply: no inventory witness; the plan cannot be checked against what the scan saw (re-run with the inventory.json that import plan consumed): %w", ErrPlanNotWitnessed)
	}
	if plan.InventoryDigest == "" {
		return fmt.Errorf("import apply: the plan records no inventory digest; regenerate it with substrate import plan: %w", ErrPlanNotWitnessed)
	}
	if plan.InventoryDigest != w.Digest {
		return fmt.Errorf("import apply: the plan was built from inventory %s, but the inventory supplied is %s: %w", truncHash(plan.InventoryDigest), truncHash(w.Digest), ErrPlanNotWitnessed)
	}

	seen := make(map[string]struct{}, len(w.Blocks))
	hashes := make(map[string]struct{}, len(w.Blocks))
	for _, ref := range w.Blocks {
		seen[blockRefKey(ref.Rel, ref.Heading, ref.Ordinal, ref.Hash)] = struct{}{}
		hashes[ref.Hash] = struct{}{}
	}

	// Refusals name where the block claimed to be and its hash, never its body:
	// the body is the untrusted payload, and echoing it into an operator's
	// terminal and logs is how a planted directive gets read anyway.
	for _, b := range plan.Blocks {
		if sha256Hex([]byte(b.Body)) != b.Hash {
			return fmt.Errorf("import apply: block %q#%q ordinal %d: body does not match its hash %s: %w", b.Rel, b.Heading, b.Ordinal, truncHash(b.Hash), ErrPlanNotWitnessed)
		}
		if _, ok := seen[blockRefKey(b.Rel, b.Heading, b.Ordinal, b.Hash)]; !ok {
			return fmt.Errorf("import apply: block %q#%q ordinal %d (hash %s) is not in the inventory the plan was built from: %w", b.Rel, b.Heading, b.Ordinal, truncHash(b.Hash), ErrPlanNotWitnessed)
		}
	}

	// A conflict side carries a body into the review queue, one approval away
	// from active, so it is checked too. Only the hash can be checked here:
	// Conflict names its slot as one joined string and a heading may itself
	// contain "#", so the pair cannot be split back into (rel, heading)
	// without guessing. Membership by hash still requires the scanner to have
	// seen the body somewhere on disk, which is what the review then shows.
	for _, c := range plan.Conflicts {
		for _, side := range c.Pair {
			if sha256Hex([]byte(side.Body)) != side.Hash {
				return fmt.Errorf("import apply: conflict slot %q: a side's body does not match its hash %s: %w", c.Slot, truncHash(side.Hash), ErrPlanNotWitnessed)
			}
			if _, ok := hashes[side.Hash]; !ok {
				return fmt.Errorf("import apply: conflict slot %q ordinal %d (hash %s) is not in the inventory the plan was built from: %w", c.Slot, side.Ordinal, truncHash(side.Hash), ErrPlanNotWitnessed)
			}
		}
	}

	mem := make(map[string]struct{}, len(w.Memories))
	for _, ref := range w.Memories {
		mem[ref.Hostname+"\x00"+ref.Hash] = struct{}{}
	}
	for _, m := range plan.Memories {
		if sha256Hex([]byte(m.Body)) != m.Hash {
			return fmt.Errorf("import apply: memory %s from %q: body does not match its hash: %w", truncHash(m.Hash), m.Hostname, ErrPlanNotWitnessed)
		}
		if _, ok := mem[m.Hostname+"\x00"+m.Hash]; !ok {
			return fmt.Errorf("import apply: memory %s from %q is not in the inventory the plan was built from: %w", truncHash(m.Hash), m.Hostname, ErrPlanNotWitnessed)
		}
	}
	return nil
}
