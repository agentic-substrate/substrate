package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/importer"
	"github.com/agentic-substrate/substrate/internal/store"
)

type decideRow struct {
	Kind string `json:"kind"`
	Body string `json:"body"`
}

type decideResult struct {
	ID       string      `json:"id"`
	Status   string      `json:"status"`
	Decision string      `json:"decision"`
	Reason   string      `json:"reason"`
	Hostname string      `json:"hostname,omitempty"`
	AsKind   string      `json:"as_kind,omitempty"`
	DryRun   bool        `json:"dry_run,omitempty"`
	Activate []decideRow `json:"activate"`
	Retire   []decideRow `json:"retire"`
}

type decideInput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Hostname string `json:"hostname"`
	AsKind   string `json:"as_kind"`
	DryRun   bool   `json:"dry_run"`
	Commit   *bool  `json:"commit"`
}

func (in decideInput) committing() bool {
	if in.DryRun {
		return false
	}
	if in.Commit != nil {
		return *in.Commit
	}
	return true
}

func planReviewDecision(ctx context.Context, tx pgx.Tx, id uuid.UUID, in decideInput, status store.ReviewStatus) (decideResult, error) {
	item, err := store.New(tx).GetReviewItem(ctx, pgUUID(id))
	if err != nil {
		return decideResult{}, err
	}
	if item.Status != store.ReviewStatusOpen {
		return decideResult{}, errReviewNotDecidable
	}
	out := decideResult{
		ID:       uuid.UUID(item.ID.Bytes).String(),
		Status:   string(item.Status),
		Decision: in.Decision,
		Reason:   in.Reason,
		Hostname: in.Hostname,
		AsKind:   in.AsKind,
		Activate: []decideRow{},
		Retire:   []decideRow{},
	}
	if item.Kind != store.ReviewKindImportConflict {
		return out, nil
	}
	var payload struct {
		Slot string                  `json:"slot"`
		Pair []importer.ConflictSide `json:"pair"`
	}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return decideResult{}, fmt.Errorf("review payload: %w", err)
	}
	if status == store.ReviewStatusApproved {
		side, ok := sideForHost(payload.Pair, in.Hostname)
		if !ok {
			return decideResult{}, fmt.Errorf("hostname %q is not a side of this conflict", in.Hostname)
		}
		asKind := strings.TrimSpace(in.AsKind)
		if asKind == "" {
			asKind = importer.Classify(importer.Block{
				Heading: headingOfSlot(payload.Slot),
				Body:    side.Body,
				Rel:     relOfSlot(payload.Slot),
			}).Kind
		}
		if asKind != "instruction" && asKind != "preference" {
			return decideResult{}, fmt.Errorf("as_kind must be instruction or preference")
		}
		out.AsKind = asKind
		out.Activate = append(out.Activate, decideRow{Kind: asKind, Body: side.Body})
		seen := map[string]bool{asKind + "\x00" + side.Body: true}
		for _, other := range payload.Pair {
			for _, k := range []string{"instruction", "preference"} {
				if k == asKind && other.Body == side.Body {
					continue
				}
				key := k + "\x00" + other.Body
				if seen[key] {
					continue
				}
				seen[key] = true
				out.Retire = append(out.Retire, decideRow{Kind: k, Body: other.Body})
			}
		}
	} else {
		seen := map[string]bool{}
		for _, side := range payload.Pair {
			for _, k := range []string{"instruction", "preference"} {
				key := k + "\x00" + side.Body
				if seen[key] {
					continue
				}
				seen[key] = true
				out.Retire = append(out.Retire, decideRow{Kind: k, Body: side.Body})
			}
		}
	}
	sortDecideRows(out.Activate)
	sortDecideRows(out.Retire)
	return out, nil
}

var errReviewNotDecidable = fmt.Errorf("review not decidable by this principal")

func isDecideBadRequest(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "hostname") || strings.Contains(msg, "as_kind")
}

func commitReviewDecision(ctx context.Context, tx pgx.Tx, p *identity.Principal, id uuid.UUID, status store.ReviewStatus, reason string, out *decideResult) error {
	q := store.New(tx)
	row, err := q.DecideReviewItem(ctx, store.DecideReviewItemParams{
		ID:        pgUUID(id),
		Status:    status,
		DecidedBy: pgUUID(p.ID),
		Reason:    &reason,
	})
	if err != nil {
		return err
	}
	out.Status = string(row.Status)
	if len(out.Activate) == 0 && len(out.Retire) == 0 {
		return nil
	}
	item, err := q.GetReviewItem(ctx, pgUUID(id))
	if err != nil {
		return err
	}
	if !item.TeamID.Valid {
		return fmt.Errorf("review decide: review_item has no team_id")
	}
	leaf := uuid.UUID(item.ScopeID.Bytes)
	teamID := uuid.UUID(item.TeamID.Bytes)
	prefScope, err := preferenceScope(ctx, q, leaf)
	if err != nil {
		return err
	}
	scopeIDs := decideScopeIDs(leaf, prefScope)
	bodies := make([]string, 0, len(out.Activate)+len(out.Retire))
	for _, r := range out.Activate {
		bodies = append(bodies, r.Body)
	}
	for _, r := range out.Retire {
		bodies = append(bodies, r.Body)
	}
	ins, err := q.ListInstructionsByBodies(ctx, store.ListInstructionsByBodiesParams{
		Bodies:   bodies,
		ScopeIds: pgUUIDs(scopeIDs),
		TeamID:   pgUUID(teamID),
	})
	if err != nil {
		return err
	}
	prefs, err := q.ListPreferencesByBodies(ctx, store.ListPreferencesByBodiesParams{
		Bodies:   bodies,
		ScopeIds: pgUUIDs(scopeIDs),
		TeamID:   pgUUID(teamID),
	})
	if err != nil {
		return err
	}
	insByBody, err := indexInstructionBodies(ins)
	if err != nil {
		return err
	}
	prefByBody, err := indexPreferenceBodies(prefs)
	if err != nil {
		return err
	}
	apply := func(kind, body string, st store.InstructionStatus) (int64, error) {
		switch kind {
		case "instruction":
			return q.ReviewApplyInstructionStatus(ctx, store.ReviewApplyInstructionStatusParams{
				Body:     body,
				Status:   st,
				ScopeIds: pgUUIDs(scopeIDs),
				TeamID:   pgUUID(teamID),
			})
		case "preference":
			return q.ReviewApplyPreferenceStatus(ctx, store.ReviewApplyPreferenceStatusParams{
				Body:     body,
				Status:   st,
				ScopeIds: pgUUIDs(scopeIDs),
				TeamID:   pgUUID(teamID),
			})
		default:
			return 0, fmt.Errorf("review decide: unknown kind %q", kind)
		}
	}
	// A kind flip needs to know the row it is flipping exists. The
	// ListBodies lookups above run under the decider's RLS session, and an
	// imported preference is owner-visible (#86), so a lead flipping someone
	// else's row sees nothing there. The retire pass just above runs through
	// the SECURITY DEFINER review_apply_* functions, which do see it, so its
	// row counts are the authoritative existence signal. The flipped row's
	// key still falls back to decideKey's hash form in that case, because the
	// original key is only readable through the same blocked lookup.
	retired := map[string]bool{}
	for _, r := range out.Retire {
		n, err := apply(r.Kind, r.Body, store.InstructionStatusRetired)
		if err != nil {
			return err
		}
		if n > 1 {
			return fmt.Errorf("review decide: matched %d %s rows for body", n, r.Kind)
		}
		if n == 1 {
			retired[r.Kind+"\x00"+r.Body] = true
		}
	}
	for _, r := range out.Activate {
		n, err := apply(r.Kind, r.Body, store.InstructionStatusActive)
		if err != nil {
			return err
		}
		if n > 1 {
			return fmt.Errorf("review decide: matched %d %s rows for body", n, r.Kind)
		}
		if n == 1 {
			continue
		}
		insOK := hasBody(insByBody, r.Body) || retired["instruction\x00"+r.Body]
		prefOK := hasBody(prefByBody, r.Body) || retired["preference\x00"+r.Body]
		otherExists := (r.Kind == "instruction" && prefOK) || (r.Kind == "preference" && insOK)
		if !otherExists {
			return fmt.Errorf("review decide: matched no row")
		}
		key := decideKey(keyForBody(r.Body, insByBody, prefByBody), r.Kind, r.Body)
		nid, err := uuid.NewV7()
		if err != nil {
			return err
		}
		switch r.Kind {
		case "instruction":
			if _, err := q.InsertInstruction(ctx, store.InsertInstructionParams{
				ID:         pgUUID(nid),
				ScopeID:    pgUUID(leaf),
				Visibility: store.VisibilityTeam,
				OwnerID:    pgUUID(p.ID),
				Kind:       store.InstructionKindRule,
				Key:        key,
				Body:       r.Body,
				Status:     store.InstructionStatusActive,
				CreatedBy:  pgUUID(p.ID),
			}); err != nil {
				return err
			}
		case "preference":
			if _, err := q.InsertPreference(ctx, store.InsertPreferenceParams{
				ID:         pgUUID(nid),
				ScopeID:    pgUUID(prefScope),
				Visibility: store.VisibilityTeam,
				OwnerID:    pgUUID(p.ID),
				Key:        key,
				Body:       r.Body,
				Status:     store.InstructionStatusActive,
				CreatedBy:  pgUUID(p.ID),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func sideForHost(pair []importer.ConflictSide, host string) (importer.ConflictSide, bool) {
	if strings.TrimSpace(host) == "" {
		return importer.ConflictSide{}, false
	}
	for _, side := range pair {
		for _, h := range side.Hostnames {
			if h == host {
				return side, true
			}
		}
	}
	return importer.ConflictSide{}, false
}

func preferenceScope(ctx context.Context, q *store.Queries, leaf uuid.UUID) (uuid.UUID, error) {
	id := leaf
	for range 8 {
		row, err := q.GetScope(ctx, pgUUID(id))
		if err != nil {
			return uuid.Nil, fmt.Errorf("review decide: scope: %w", err)
		}
		switch row.Kind {
		case store.ScopeKindTeam, store.ScopeKindOrg, store.ScopeKindUser:
			return id, nil
		}
		if !row.ParentID.Valid {
			break
		}
		id = uuid.UUID(row.ParentID.Bytes)
	}
	return uuid.Nil, fmt.Errorf("review decide: no team/org/user ancestor of %s", leaf)
}

func hasBody[T any](m map[string]T, body string) bool {
	_, ok := m[body]
	return ok
}

func keyForBody(body string, ins map[string]store.ListInstructionsByBodiesRow, pref map[string]store.ListPreferencesByBodiesRow) string {
	if r, ok := ins[body]; ok {
		return r.Key
	}
	if r, ok := pref[body]; ok {
		return r.Key
	}
	return ""
}

func decideScopeIDs(leaf, pref uuid.UUID) []uuid.UUID {
	if pref == uuid.Nil || pref == leaf {
		return []uuid.UUID{leaf}
	}
	return []uuid.UUID{leaf, pref}
}

func pgUUIDs(ids []uuid.UUID) []pgtype.UUID {
	out := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		out[i] = pgUUID(id)
	}
	return out
}

func indexInstructionBodies(rows []store.ListInstructionsByBodiesRow) (map[string]store.ListInstructionsByBodiesRow, error) {
	out := map[string]store.ListInstructionsByBodiesRow{}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Body]++
		out[r.Body] = r
	}
	for body, n := range counts {
		if n > 1 {
			return nil, fmt.Errorf("review decide: matched %d instruction rows for body %q", n, body)
		}
	}
	return out, nil
}

func indexPreferenceBodies(rows []store.ListPreferencesByBodiesRow) (map[string]store.ListPreferencesByBodiesRow, error) {
	out := map[string]store.ListPreferencesByBodiesRow{}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Body]++
		out[r.Body] = r
	}
	for body, n := range counts {
		if n > 1 {
			return nil, fmt.Errorf("review decide: matched %d preference rows for body %q", n, body)
		}
	}
	return out, nil
}

func decideKey(existing, asKind, body string) string {
	if existing != "" {
		for _, k := range []string{"instruction", "preference"} {
			old := "import." + k + "."
			if strings.HasPrefix(existing, old) {
				return "import." + asKind + "." + strings.TrimPrefix(existing, old)
			}
		}
	}
	sum := sha256.Sum256([]byte(body))
	return "import." + asKind + ".review." + hex.EncodeToString(sum[:6])
}

func headingOfSlot(slot string) string {
	_, h, ok := strings.Cut(slot, "#")
	if !ok {
		return slot
	}
	return h
}

func relOfSlot(slot string) string {
	rel, _, ok := strings.Cut(slot, "#")
	if !ok {
		return slot
	}
	return rel
}

func sortDecideRows(rows []decideRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Body < rows[j].Body
	})
}
