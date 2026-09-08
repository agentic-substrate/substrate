package memory

// SourceIn is write provenance. machine is required (MEM-1).
type SourceIn struct {
	Machine string `json:"machine" jsonschema:"machine the write originated on"`
}

// VerificationIn is the write verification object. type is required (MEM-1).
type VerificationIn struct {
	Type string `json:"type" jsonschema:"code, human, or agent_inference"`
}

// WriteIn is the memory.write input (EDD §4.1). source is optional on the
// wire so the schema matches the contract; the handler still rejects a write
// that omits it (MEM-1).
type WriteIn struct {
	Kind         string         `json:"kind" jsonschema:"fact, decision, incident, lesson, or observation"`
	Title        string         `json:"title" jsonschema:"short title"`
	Body         string         `json:"body" jsonschema:"memory body"`
	Scope        string         `json:"scope" jsonschema:"scope path such as global:/org:acme/team:core"`
	Visibility   string         `json:"visibility,omitempty" jsonschema:"owner, team, org, or global"`
	Identifiers  []string       `json:"identifiers,omitempty" jsonschema:"extracted paths, symbols, error strings, issue ids"`
	Verification VerificationIn `json:"verification" jsonschema:"verification object; type is required"`
	Source       *SourceIn      `json:"source,omitempty" jsonschema:"provenance; machine is required"`
	Status       string         `json:"status,omitempty" jsonschema:"requested status; ignored for agent principals"`
	Tier         string         `json:"tier,omitempty" jsonschema:"working, episodic, or semantic"`
}

// WriteOut is the memory.write result.
type WriteOut struct {
	ID     string `json:"id" jsonschema:"id of the stored memory"`
	Status string `json:"status" jsonschema:"status actually stored"`
}

// MaxSearchLimit is the hard ceiling for SearchIn.Limit. Values above it are
// clamped silently — callers that need more rows must page or narrow filters.
const MaxSearchLimit = 100

// SearchIn is the memory.search input (EDD §4.1).
type SearchIn struct {
	Query  string   `json:"query" jsonschema:"search text; exact identifier hits outrank keyword"`
	Scope  string   `json:"scope,omitempty" jsonschema:"optional scope path filter"`
	Tier   string   `json:"tier,omitempty" jsonschema:"working, episodic, or semantic; default semantic"`
	Status []string `json:"status,omitempty" jsonschema:"status filter; default confirmed and probable"`
	Limit  int      `json:"limit,omitempty" jsonschema:"max results; default 20"`
}

// SearchHit is one memory.search result.
type SearchHit struct {
	ID      string  `json:"id" jsonschema:"memory id"`
	Title   string  `json:"title" jsonschema:"memory title"`
	Snippet string  `json:"snippet" jsonschema:"short body excerpt"`
	Status  string  `json:"status" jsonschema:"stored status"`
	Scope   string  `json:"scope" jsonschema:"scope id"`
	Score   float64 `json:"score" jsonschema:"ranking score; identifier hits outrank keyword"`
}

// SearchOut is the memory.search result.
type SearchOut struct {
	Results []SearchHit `json:"results" jsonschema:"ranked hits"`
}

// SupersedeIn is the memory.supersede input (EDD §4.1).
type SupersedeIn struct {
	OldID  string `json:"old_id" jsonschema:"id of the memory to supersede"`
	Body   string `json:"body" jsonschema:"replacement body"`
	Reason string `json:"reason" jsonschema:"audit reason; required"`
}

// SupersedeOut is the memory.supersede result.
type SupersedeOut struct {
	ID string `json:"id" jsonschema:"id of the new memory"`
}
