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
	store func() *store.Store
}

// New returns a Service that loads the store per request so /mcp can serve
// before Postgres is ready.
func New(getStore func() *store.Store) *Service {
	if getStore == nil {
		getStore = func() *store.Store { return nil }
	}
	return &Service{store: getStore}
}

// Register attaches memory.write, memory.search, and memory.supersede.
func Register(s *mcpx.Server, getStore func() *store.Store) {
	svc := New(getStore)
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
