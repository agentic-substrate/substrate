package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Write stores a memory. Agents are forced to unverified regardless of the
// requested status (Gotcha 4). Missing scope, source, or verification is
// rejected with a message naming the field (MEM-1).
func (s *Service) Write(ctx context.Context, in WriteIn) (WriteOut, error) {
	if err := validateWrite(in); err != nil {
		return WriteOut{}, err
	}

	p := identity.FromContext(ctx)
	if p == nil {
		return WriteOut{}, store.ErrNoPrincipal
	}
	if err := agentVerification(p, in.Verification.Type); err != nil {
		return WriteOut{}, err
	}

	sc, err := scope.Parse(in.Scope)
	if err != nil {
		return WriteOut{}, fmt.Errorf("memory.write: scope: %w", err)
	}
	kind, err := parseKind(in.Kind)
	if err != nil {
		return WriteOut{}, err
	}
	vis, err := parseVisibility(in.Visibility)
	if err != nil {
		return WriteOut{}, err
	}
	tier, err := parseTier(in.Tier)
	if err != nil {
		return WriteOut{}, err
	}
	status := decideStatus(p, in.Status)

	// Strip and cap before the scan so the scanner sees the bytes that land
	// in the row. Scanning the raw input lets a control character split a
	// key, then stripControls reconstitutes it in storage.
	body := capBody(stripControls(in.Body))
	title := stripControls(in.Title)
	idIn := make([]string, 0, len(in.Identifiers))
	for _, id := range in.Identifiers {
		idIn = append(idIn, stripControls(id))
	}
	ids := extractIdentifiers(body, idIn)
	machine := ""
	if in.Source != nil {
		machine = in.Source.Machine
	}
	if err := scanStored(title, body, in.Kind, in.Verification.Type, machine, ids); err != nil {
		return WriteOut{}, err
	}

	src, err := json.Marshal(map[string]string{"machine": in.Source.Machine})
	if err != nil {
		return WriteOut{}, fmt.Errorf("memory.write: source: %w", err)
	}
	ver, err := json.Marshal(map[string]string{"type": in.Verification.Type})
	if err != nil {
		return WriteOut{}, fmt.Errorf("memory.write: verification: %w", err)
	}

	st, err := s.requireStore()
	if err != nil {
		return WriteOut{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return WriteOut{}, fmt.Errorf("memory.write: %w", err)
	}

	var stored store.MemoryStatus
	err = st.TxChecked(ctx, "memory.write", sc, func(tx pgx.Tx) error {
		q := store.New(tx)
		scopeID, err := resolveScopeID(ctx, q, sc)
		if err != nil {
			return err
		}
		row, err := q.InsertMemory(ctx, store.InsertMemoryParams{
			ID:           pgUUID(id),
			ScopeID:      pgUUID(scopeID),
			Visibility:   vis,
			OwnerID:      pgUUID(p.ID),
			Tier:         tier,
			Kind:         kind,
			Title:        title,
			Body:         body,
			Identifiers:  ids,
			Status:       status,
			Source:       src,
			Verification: ver,
			CreatedBy:    pgUUID(p.ID),
		})
		if err != nil {
			return fmt.Errorf("memory.write: %w", err)
		}
		stored = row.Status
		return nil
	})
	if err != nil {
		return WriteOut{}, err
	}
	// After the row is durable. A failure here leaves embedding NULL for the
	// backfill job rather than failing a write that already succeeded — losing
	// the memory because the GPU is busy would be the worse outcome (EDD §8.4).
	_ = s.storeEmbedding(ctx, st, id.String(), title, body)
	return WriteOut{ID: id.String(), Status: string(stored)}, nil
}

func validateWrite(in WriteIn) error {
	if strings.TrimSpace(in.Scope) == "" {
		return fmt.Errorf("memory.write: missing scope")
	}
	if in.Source == nil || strings.TrimSpace(in.Source.Machine) == "" {
		return fmt.Errorf("memory.write: missing source")
	}
	if strings.TrimSpace(in.Verification.Type) == "" {
		return fmt.Errorf("memory.write: missing verification")
	}
	return nil
}

func scanStored(title, body, kind, verification, machine string, ids []string) error {
	parts := []string{title, body, kind, verification, machine}
	parts = append(parts, ids...)
	return scanAll(parts...)
}

func decideStatus(p *identity.Principal, requested string) store.MemoryStatus {
	if isAgent(p) {
		return store.MemoryStatusUnverified
	}
	switch requested {
	case string(store.MemoryStatusConfirmed):
		return store.MemoryStatusConfirmed
	case string(store.MemoryStatusProbable):
		return store.MemoryStatusProbable
	case string(store.MemoryStatusUnverified), "":
		return store.MemoryStatusUnverified
	default:
		return store.MemoryStatusUnverified
	}
}

func isAgent(p *identity.Principal) bool {
	if p == nil {
		return false
	}
	return p.Kind == identity.KindAgent ||
		p.Trust == identity.TrustAgentInteractive ||
		p.Trust == identity.TrustAgentAutonomous
}

func agentVerification(p *identity.Principal, typ string) error {
	if !isAgent(p) {
		return nil
	}
	switch typ {
	case "agent_inference", "code":
		return nil
	default:
		return fmt.Errorf("memory.write: agent verification.type must be agent_inference or code")
	}
}

func parseKind(s string) (store.MemoryKind, error) {
	k := store.MemoryKind(s)
	switch k {
	case store.MemoryKindFact, store.MemoryKindDecision, store.MemoryKindIncident, store.MemoryKindLesson, store.MemoryKindObservation:
		return k, nil
	default:
		return "", fmt.Errorf("memory.write: unknown kind %q", s)
	}
}

func parseVisibility(s string) (store.Visibility, error) {
	if s == "" {
		return store.VisibilityTeam, nil
	}
	v := store.Visibility(s)
	switch v {
	case store.VisibilityOwner, store.VisibilityTeam, store.VisibilityOrg, store.VisibilityGlobal:
		return v, nil
	default:
		return "", fmt.Errorf("memory.write: unknown visibility %q", s)
	}
}

func parseTier(s string) (store.MemoryTier, error) {
	if s == "" {
		return store.MemoryTierSemantic, nil
	}
	t := store.MemoryTier(s)
	switch t {
	case store.MemoryTierWorking, store.MemoryTierEpisodic, store.MemoryTierSemantic:
		return t, nil
	default:
		return "", fmt.Errorf("memory.write: unknown tier %q", s)
	}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func resolveScopeID(ctx context.Context, q *store.Queries, p scope.Path) (uuid.UUID, error) {
	var parent pgtype.UUID
	var last uuid.UUID
	for i := range p {
		row, err := q.LookupScope(ctx, store.LookupScopeParams{
			Kind:     store.ScopeKind(p[i].Kind),
			Key:      p[i].Name,
			ParentID: parent,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("unknown scope %s: %w", p[:i+1].String(), err)
		}
		last = uuid.UUID(row.ID.Bytes)
		parent = row.ID
	}
	return last, nil
}
