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
type Plan struct {
	Blocks    []Block    `json:"blocks"`
	Conflicts []Conflict `json:"conflicts"`
}

// Block is one content-hash-unique markdown block after classification.
type Block struct {
	Hash         string   `json:"hash"`
	Heading      string   `json:"heading"`
	Body         string   `json:"body"`
	Kind         string   `json:"kind"`
	Confidence   float64  `json:"confidence"`
	Note         string   `json:"note"`
	ImpliedScope string   `json:"implied_scope"`
	DetectedType string   `json:"detected_type"`
	Rel          string   `json:"rel"`
	Sources      []Source `json:"sources"`
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
}

// Classification is a heuristic kind with an honest confidence.
type Classification struct {
	Kind       string
	Confidence float64
	Note       string
}

// Classifier labels a block. Tests inject one to prove dedupe runs first.
type Classifier func(Block) Classification
