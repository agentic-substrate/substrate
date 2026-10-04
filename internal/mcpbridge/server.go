package mcpbridge

import (
	"context"
	"encoding/json"
	"io"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CaptureInput struct {
	OperationID      string                 `json:"operation_id" jsonschema:"stable unique operation ID; identical retries return the original receipt"`
	Content          string                 `json:"content" jsonschema:"unverified memory observation; at most one MiB of UTF-8 text"`
	Provenance       string                 `json:"provenance" jsonschema:"evidence or source claim; at most 4096 bytes"`
	ArtifactID       string                 `json:"artifact_id,omitempty"`
	ExpectedRevision string                 `json:"expected_revision,omitempty"`
	Associations     artifacts.Associations `json:"associations,omitzero"`
}
type Connection struct{ StateDir, Checkout, Credential string }

func (c Connection) call(ctx context.Context, req node.Request) (*mcp.CallToolResult, any, error) {
	token, err := authority.ReadCredential(c.Credential)
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "session credential unavailable: use a private scoped credential file"}}}, nil, nil
	}
	req.Token, req.Checkout = token, c.Checkout
	result := node.Call(ctx, c.StateDir, req)
	if result.Error != "" {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: result.Error}}}, nil, nil
	}
	var output any
	if err := json.Unmarshal(result.Result, &output); err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(result.Result)}}}, output, nil
}
func New(c Connection) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "substrate", Version: "0.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "discovery", Description: "List current permitted memory and approved Git snapshots in this fixed session. Ranking cannot approve or activate source. Reports index coverage."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return c.call(ctx, node.Request{Action: "discovery"})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "capture", Description: "Durably save an unverified memory in the fixed authorized repository. Does not approve instructions or publish across spaces. Requires stable operation_id and provenance."}, func(ctx context.Context, _ *mcp.CallToolRequest, in CaptureInput) (*mcp.CallToolResult, any, error) {
		return c.call(ctx, node.Request{Action: "capture", Contribution: artifacts.Contribution{OperationID: in.OperationID, ArtifactID: in.ArtifactID, ExpectedRevision: in.ExpectedRevision, Kind: "memory", Content: in.Content, Provenance: in.Provenance, Associations: in.Associations}})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "search", Description: "Search current permitted revisions with lexical terms, exact identifiers, explicit search aliases and topics. related_to selects one-hop results after checking both current endpoints. Optional final * permits prefixes of at least three characters. Index pending means incomplete lexical coverage; no remote/model fallback."}, func(ctx context.Context, _ *mcp.CallToolRequest, in artifacts.SearchRequest) (*mcp.CallToolResult, any, error) {
		return c.call(ctx, node.Request{Action: "search", Search: in})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "read", Description: "Read one CURRENT permitted revision by artifact_id, revision_id, or selector (exact qualified identity or explicit alias). Supply exactly one field. Denies missing, stale, retired and inaccessible objects. Native installation and execution remain unsupported."}, func(ctx context.Context, _ *mcp.CallToolRequest, in artifacts.ReadRequest) (*mcp.CallToolResult, any, error) {
		return c.call(ctx, node.Request{Action: "read", Read: in})
	})
	return server
}
func Run(ctx context.Context, c Connection, in io.Reader, out io.Writer) error {
	return New(c).Run(ctx, &mcp.IOTransport{Reader: newFrameReader(in), Writer: nopWriter{out}, MaxLineLength: node.MaxFrame})
}

type nopWriter struct{ io.Writer }

func (nopWriter) Close() error { return nil }
