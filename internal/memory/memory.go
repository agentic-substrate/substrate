// Package memory implements memory.write, memory.search, and memory.supersede
// with the Phase 1 write policy (EDD §4.1, §8.3).
package memory

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Service is the memory domain. Tools register on an mcpx.Server; tests call
// Write/Search/Supersede directly.
type Service struct {
	store    func() *store.Store
	embedder Embedder
	// FailAfterReceipt is invoked inside the batch transaction after the
	// ingest_receipt row and before the memory row so tests can abort the
	// transaction (Gotcha 10). Production leaves it nil.
	FailAfterReceipt func() error
}

// New returns a Service that loads the store per request so /mcp can serve
// before Postgres is ready.
func New(getStore func() *store.Store) *Service {
	if getStore == nil {
		getStore = func() *store.Store { return nil }
	}
	return &Service{store: getStore}
}

// WithEmbedder returns s using e for semantic search and write-path
// vectorisation. Without one the service is keyword-only, which is a supported
// configuration: search must never fail because embeddings are unavailable
// (MEM-5).
func (s *Service) WithEmbedder(e Embedder) *Service {
	s.embedder = e
	return s
}

// Register attaches memory.write, memory.search, and memory.supersede. A nil
// embedder is valid and means keyword-only retrieval (MEM-5).
//
// It returns the Service so a caller can assert what was actually wired: the
// embedder being dropped here is invisible at runtime (search silently falls
// back to keyword-only) and invisible in tests that construct their own
// Service.
func Register(s *mcpx.Server, getStore func() *store.Store, e Embedder) *Service {
	svc := New(getStore)
	if e != nil {
		svc = svc.WithEmbedder(e)
	}
	mcpx.AddTool(s, &mcp.Tool{
		Name:        "memory.write",
		Description: "Store a memory. Agents are always unverified. Call when a fact, decision, incident, lesson, or observation should persist across sessions.",
	}, svc.writeTool)
	mcpx.AddTool(s, &mcp.Tool{
		Name:        "memory.search",
		Description: "Search memories by keyword and identifier. Exact identifier hits outrank keyword. Default: semantic confirmed/probable. Call before answering from prior work.",
	}, svc.searchTool)
	mcpx.AddTool(s, &mcp.Tool{
		Name:        "memory.supersede",
		Description: "Replace a memory's body without deleting the old row. Requires an audit reason. Call when a stored fact is outdated.",
	}, svc.supersedeTool)
	return svc
}

func (s *Service) writeTool(ctx context.Context, _ *mcp.CallToolRequest, in WriteIn) (*mcp.CallToolResult, WriteOut, error) {
	out, err := s.Write(ctx, in)
	return nil, out, err
}

func (s *Service) searchTool(ctx context.Context, _ *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, SearchOut, error) {
	out, err := s.Search(ctx, in)
	return nil, out, err
}

func (s *Service) supersedeTool(ctx context.Context, _ *mcp.CallToolRequest, in SupersedeIn) (*mcp.CallToolResult, SupersedeOut, error) {
	out, err := s.Supersede(ctx, in)
	return nil, out, err
}

func (s *Service) requireStore() (*store.Store, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("store not configured")
	}
	st := s.store()
	if st == nil {
		return nil, fmt.Errorf("store not configured")
	}
	return st, nil
}
