// Package compiler builds the context pack for context.get (CTX-1, CTX-2).
// Instructions, preferences, and mandatory items are never trimmed.
package compiler

import (
	"github.com/google/uuid"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Default section caps (EDD §5.2). The request budget is the overall cap.
const (
	DefaultInstructions = 1500
	DefaultPreferences  = 500
	DefaultMandatory    = 1500
	DefaultMemory       = 6000
	DefaultSkills       = 2000
	DefaultTask         = 3000
	MemoryItemCap       = 20
)

// DefaultBudget is the sum of the documented per-section defaults.
func DefaultBudget() int {
	return DefaultInstructions + DefaultPreferences + DefaultMandatory + DefaultMemory + DefaultSkills + DefaultTask
}

// Request is the compiler input (EDD §5.1).
type Request struct {
	Scope  scope.Path
	TaskID *uuid.UUID
	Files  []string
	Budget int
}

// Section is one named region of the pack, with its estimated tokens.
type Section struct {
	Name   string `json:"name" jsonschema:"section name"`
	Tokens int    `json:"tokens" jsonschema:"conservative token estimate for the section"`
}

// Item is one pack member listed in the structured output.
type Item struct {
	ID     string `json:"id" jsonschema:"item id"`
	Type   string `json:"type" jsonschema:"instruction, preference, mandatory, memory, or skill"`
	Status string `json:"status" jsonschema:"item status"`
}

// Pack is the context.get result (EDD §4.1).
type Pack struct {
	PackID   string    `json:"pack_id" jsonschema:"id of this pack"`
	Markdown string    `json:"markdown" jsonschema:"rendered pack"`
	Sections []Section `json:"sections" jsonschema:"per-section token estimates"`
	Items    []Item    `json:"items" jsonschema:"items included in the pack"`
}

// Service compiles packs. Tools register on an mcpx.Server.
type Service struct {
	store func() *store.Store
	mem   *memory.Service
}

// New returns a Service. mem may be nil; the memory section is then empty.
func New(getStore func() *store.Store, mem *memory.Service) *Service {
	if getStore == nil {
		getStore = func() *store.Store { return nil }
	}
	return &Service{store: getStore, mem: mem}
}

func (s *Service) requireStore() (*store.Store, error) {
	if s == nil || s.store == nil {
		return nil, errStore
	}
	st := s.store()
	if st == nil {
		return nil, errStore
	}
	return st, nil
}

func principalOrErr(p *identity.Principal) error {
	if p == nil {
		return store.ErrNoPrincipal
	}
	return nil
}
