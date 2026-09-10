// Package importer inventories harness config and plans a read-only import
// (SYNC-5, EDD §9). Classification is heuristic and will misfile (R15).
package importer

// Request is a Scan input. Roots must be absolute; there is no $HOME default.
// MemorixJSON is an optional absolute path to a file the operator exported
// beforehand; scan never execs the memorix binary.
type Request struct {
	Roots       []string
	Hostname    string
	MemorixJSON string
	// Exclude are glob patterns matched against a file's slash-separated path
	// relative to its root. "**" matches across separators. A pattern that
	// does not compile is an error, never a silent no-op: a typo that
	// excluded nothing would be indistinguishable from a working filter.
	Exclude []string
}

// Inventory is the scan product written as inventory.json.
type Inventory struct {
	Hostname string    `json:"hostname"`
	Roots    []string  `json:"roots"`
	Files    []File    `json:"files"`
	Skipped  []Skipped `json:"skipped"`
}

// File is one harness config file (or a raw Memorix export).
type File struct {
	Path         string `json:"path"`
	Rel          string `json:"rel"`
	Size         int64  `json:"size"`
	Hash         string `json:"hash"`
	DetectedType string `json:"detected_type"`
	ImpliedScope string `json:"implied_scope"`
	Content      string `json:"content"`
}

// Skipped is a source that was not inventoried, with the reason.
type Skipped struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Plan is the plan.json product: unique blocks plus conflict pairs.
//
// InventoryDigest names the inventory the plan was built from (#95). Apply
// refuses a plan whose digest does not match the inventory witness it is given,
// and refuses any block that witness does not contain.
type Plan struct {
	InventoryDigest string       `json:"inventory_digest,omitempty"`
	Blocks          []Block      `json:"blocks"`
	Conflicts       []Conflict   `json:"conflicts"`
	Memories        []MemoryItem `json:"memories,omitempty"`
}

// MemoryItem is one memorix-exported memory. SourceStatus is what the export
// claimed; Apply must ignore it (Gotcha 4).
type MemoryItem struct {
	Hash         string `json:"hash"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	Kind         string `json:"kind"`
	Hostname     string `json:"hostname"`
	SourceStatus string `json:"source_status,omitempty"`
}

// ApplyRequest is one POST /v1/import (or CLI apply) invocation.
//
// Witness is what the scan saw on disk, derived from inventory.json by the
// caller and never from the plan. It is the only reason a block in Plan is
// admissible (#95); an empty witness refuses the whole apply.
type ApplyRequest struct {
	Plan           Plan
	Witness        InventoryWitness
	Machine        string
	TrustedMachine string
	Scope          string
	Commit         bool
	ClientID       string
}

// PlannedRow is one row the apply would insert, used by dry-run and commit.
type PlannedRow struct {
	Hostname string         `json:"hostname"`
	Kind     string         `json:"kind"`
	Status   string         `json:"status"`
	Key      string         `json:"key,omitempty"`
	Hash     string         `json:"hash"`
	Body     string         `json:"body"`
	Title    string         `json:"title,omitempty"`
	Slot     string         `json:"slot,omitempty"`
	Pair     []ConflictSide `json:"pair,omitempty"`
	// Scope and Visibility are what the commit will file this row at. They
	// are filled during planning so the dry-run preview can show them
	// before --commit, not only after (#86).
	Scope      string `json:"scope,omitempty"`
	Visibility string `json:"visibility,omitempty"`
}

// HostSummary counts planned writes for one hostname.
type HostSummary struct {
	Active   int `json:"active"`
	Proposed int `json:"proposed"`
	Conflict int `json:"conflict"`
	Memory   int `json:"memory"`
}

// ApplyResult is the dry-run preview and the commit report.
type ApplyResult struct {
	DryRun     bool                   `json:"dry_run"`
	Duplicate  bool                   `json:"duplicate,omitempty"`
	Active     []PlannedRow           `json:"active"`
	Proposed   []PlannedRow           `json:"proposed"`
	Conflict   []PlannedRow           `json:"conflict"`
	Memory     []PlannedRow           `json:"memory"`
	Skipped    []Skipped              `json:"skipped,omitempty"`
	ByHostname map[string]HostSummary `json:"by_hostname"`
}

// Block is one content-hash-unique markdown block after classification.
type Block struct {
	Hash         string  `json:"hash"`
	Heading      string  `json:"heading"`
	Body         string  `json:"body"`
	Kind         string  `json:"kind"`
	Confidence   float64 `json:"confidence"`
	Note         string  `json:"note"`
	ImpliedScope string  `json:"implied_scope"`
	DetectedType string  `json:"detected_type"`
	Rel          string  `json:"rel"`
	// Ordinal is the block's position within its (rel, heading) slot in source
	// order. It keys the instruction instead of a content hash so that editing
	// one bullet does not mint a new rule (#80). Reordering bullets does
	// re-key them — the accepted limitation.
	Ordinal int      `json:"ordinal"`
	Sources []Source `json:"sources"`
}

// Source is one machine/path a block was observed on.
type Source struct {
	Hostname string `json:"hostname"`
	Path     string `json:"path"`
	Rel      string `json:"rel"`
}

// Conflict is one slot with more than one content hash.
type Conflict struct {
	Slot string         `json:"slot"`
	Pair []ConflictSide `json:"pair"`
}

// ConflictSide is one variant in a conflict pair, tagged with hostname.
type ConflictSide struct {
	Hash      string   `json:"hash"`
	Hostnames []string `json:"hostnames"`
	Body      string   `json:"body"`
	Ordinal   int      `json:"ordinal"`
}

// Classification is a heuristic kind with an honest confidence.
type Classification struct {
	Kind       string
	Confidence float64
	Note       string
}

// Classifier labels a block. Tests inject one to prove dedupe runs first.
type Classifier func(Block) Classification
