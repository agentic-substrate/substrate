package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

var importNS = uuid.MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c8") // URL namespace

// Apply classifies a plan against the current store and, if req.Commit, writes
// it in one transaction. Dry-run uses the same planWrites path in a READ ONLY
// transaction and performs zero writes (SYNC-5, EDD §9).
func Apply(ctx context.Context, st *store.Store, req ApplyRequest) (*ApplyResult, error) {
	if st == nil {
		return nil, fmt.Errorf("import apply: store not configured")
	}
	p := identity.FromContext(ctx)
	if p == nil {
		return nil, store.ErrNoPrincipal
	}
	if req.Machine == "" {
		return nil, fmt.Errorf("import apply: machine is required")
	}
	if req.TrustedMachine == "" {
		return nil, fmt.Errorf("import apply: trusted machine is required")
	}
	sc, err := scope.Parse(req.Scope)
	if err != nil {
		return nil, fmt.Errorf("import apply: scope: %w", err)
	}
	if err := policy.Check("import.apply", sc, *p); err != nil {
		return nil, err
	}

	run := func(tx pgx.Tx) (*ApplyResult, error) {
		res, err := planWrites(ctx, tx, p, req, sc)
		if err != nil {
			return nil, err
		}
		if !req.Commit {
			res.DryRun = true
			res.finalize()
			return res, nil
		}
		if err := commitWrites(ctx, tx, p, req, sc, res); err != nil {
			return nil, err
		}
		res.DryRun = false
		res.finalize()
		return res, nil
	}

	var res *ApplyResult
	if req.Commit {
		err = st.Tx(ctx, func(tx pgx.Tx) error {
			var e error
			res, e = run(tx)
			return e
		})
	} else {
		err = st.TxReadOnly(ctx, func(tx pgx.Tx) error {
			var e error
			res, e = run(tx)
			return e
		})
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func planWrites(ctx context.Context, tx pgx.Tx, _ *identity.Principal, req ApplyRequest, sc scope.Path) (*ApplyResult, error) {
	q := store.New(tx)
	ids, teamID, leafID, err := lookupPath(ctx, q, sc)
	if err != nil {
		return nil, err
	}
	clientID := applyClientID(req)
	existing, err := q.GetIngestReceipt(ctx, pgUUID(clientID))
	if err == nil {
		_ = existing
		res := &ApplyResult{Duplicate: true}
		res.finalize()
		return res, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("import apply: receipt: %w", err)
	}

	storedHost, err := loadTrustedHost(ctx, q, req.Scope)
	if err != nil {
		return nil, err
	}
	if storedHost != "" {
		if req.TrustedMachine != storedHost {
			return nil, fmt.Errorf("import apply: trusted machine is %q, not %q", storedHost, req.TrustedMachine)
		}
	} else if req.Machine != req.TrustedMachine {
		return nil, fmt.Errorf("import apply: trusted machine %q must be imported first", req.TrustedMachine)
	}

	activeIns, err := q.ListActiveInstructions(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("import apply: list instructions: %w", err)
	}
	activePref, err := q.ListActivePreferences(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("import apply: list preferences: %w", err)
	}
	reviews, err := q.ListOpenImportConflicts(ctx)
	if err != nil {
		return nil, fmt.Errorf("import apply: list conflicts: %w", err)
	}
	reviewSlots := map[string]bool{}
	for _, row := range reviews {
		var payload struct {
			Slot     string `json:"slot"`
			Hostname string `json:"hostname"`
		}
		_ = json.Unmarshal(row.Payload, &payload)
		if payload.Slot != "" {
			reviewSlots[payload.Slot] = true
		}
	}

	canActivate := req.Machine == req.TrustedMachine && (storedHost == "" || storedHost == req.Machine)
	res := &ApplyResult{}
	conflictHash := map[string]Conflict{}
	for _, c := range req.Plan.Conflicts {
		for _, side := range c.Pair {
			conflictHash[side.Hash] = c
		}
	}

	for _, b := range req.Plan.Blocks {
		if !blockOnMachine(b, req.Machine) {
			continue
		}
		if identicalActive(b.Body, activeIns, activePref) {
			continue
		}
		row := PlannedRow{
			Hostname: req.Machine,
			Kind:     b.Kind,
			Key:      rowKey(b.Kind, b.Rel, b.Heading, b.Hash),
			Hash:     b.Hash,
			Body:     b.Body,
		}
		if row.Kind == "" {
			row.Kind = Classify(b).Kind
		}
		if canActivate {
			row.Status = string(store.InstructionStatusActive)
			res.Active = append(res.Active, row)
		} else {
			row.Status = string(store.InstructionStatusProposed)
			res.Proposed = append(res.Proposed, row)
		}
	}

	for _, c := range req.Plan.Conflicts {
		for _, side := range c.Pair {
			if !hostIn(side.Hostnames, req.Machine) {
				continue
			}
			if identicalActive(side.Body, activeIns, activePref) {
				continue
			}
			kind := Classify(Block{Heading: headingOfSlot(c.Slot), Body: side.Body, Rel: relOfSlot(c.Slot)}).Kind
			res.Proposed = append(res.Proposed, PlannedRow{
				Hostname: req.Machine,
				Kind:     kind,
				Status:   string(store.InstructionStatusProposed),
				Key:      rowKey(kind, relOfSlot(c.Slot), headingOfSlot(c.Slot), side.Hash),
				Hash:     side.Hash,
				Body:     side.Body,
				Slot:     c.Slot,
			})
			if !reviewSlots[c.Slot] {
				res.Conflict = append(res.Conflict, PlannedRow{
					Hostname: req.Machine,
					Kind:     "import_conflict",
					Status:   "conflict",
					Hash:     side.Hash,
					Body:     side.Body,
					Slot:     c.Slot,
				})
				reviewSlots[c.Slot] = true
			}
		}
	}

	for _, m := range req.Plan.Memories {
		if m.Hostname != req.Machine {
			continue
		}
		kind := m.Kind
		if !validMemoryKind(kind) {
			kind = "observation"
		}
		res.Memory = append(res.Memory, PlannedRow{
			Hostname: req.Machine,
			Kind:     kind,
			Status:   string(store.MemoryStatusUnverified),
			Hash:     m.Hash,
			Body:     m.Body,
			Title:    m.Title,
		})
	}

	sortPlanned(res)
	_ = teamID
	_ = leafID
	return res, nil
}

func commitWrites(ctx context.Context, tx pgx.Tx, p *identity.Principal, req ApplyRequest, sc scope.Path, res *ApplyResult) error {
	if res.Duplicate {
		return nil
	}
	q := store.New(tx)
	_, teamScopeID, leafID, err := lookupPath(ctx, q, sc)
	if err != nil {
		return err
	}
	teamID := uuid.Nil
	if len(p.TeamIDs) > 0 {
		teamID = p.TeamIDs[0]
	}

	writeRow := func(row PlannedRow) error {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		switch row.Kind {
		case "preference":
			_, err = q.InsertPreference(ctx, store.InsertPreferenceParams{
				ID:         pgUUID(id),
				ScopeID:    pgUUID(teamScopeID),
				Visibility: store.VisibilityTeam,
				OwnerID:    pgUUID(p.ID),
				Key:        row.Key,
				Body:       row.Body,
				Status:     store.InstructionStatus(row.Status),
				CreatedBy:  pgUUID(p.ID),
			})
		default:
			_, err = q.InsertInstruction(ctx, store.InsertInstructionParams{
				ID:         pgUUID(id),
				ScopeID:    pgUUID(leafID),
				Visibility: store.VisibilityTeam,
				OwnerID:    pgUUID(p.ID),
				Kind:       store.InstructionKindRule,
				Key:        row.Key,
				Body:       row.Body,
				Status:     store.InstructionStatus(row.Status),
				CreatedBy:  pgUUID(p.ID),
			})
		}
		return err
	}

	for _, row := range res.Active {
		if err := writeRow(row); err != nil {
			return fmt.Errorf("import apply: active: %w", err)
		}
	}
	for _, row := range res.Proposed {
		if err := writeRow(row); err != nil {
			return fmt.Errorf("import apply: proposed: %w", err)
		}
	}
	for _, row := range res.Conflict {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		payload, err := conflictPayload(req, row)
		if err != nil {
			return err
		}
		if _, err := q.InsertReviewItem(ctx, store.InsertReviewItemParams{
			ID:         pgUUID(id),
			Kind:       store.ReviewKindImportConflict,
			ScopeID:    pgUUID(leafID),
			TeamID:     pgUUID(teamID),
			Payload:    payload,
			ProposedBy: pgUUID(p.ID),
		}); err != nil {
			return fmt.Errorf("import apply: conflict: %w", err)
		}
	}
	for _, row := range res.Memory {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		src, err := json.Marshal(map[string]string{"machine": row.Hostname})
		if err != nil {
			return err
		}
		ver, err := json.Marshal(map[string]string{"type": "human"})
		if err != nil {
			return err
		}
		title := row.Title
		if title == "" {
			title = row.Body
		}
		kind := store.MemoryKind(row.Kind)
		if _, err := q.InsertMemory(ctx, store.InsertMemoryParams{
			ID:           pgUUID(id),
			ScopeID:      pgUUID(leafID),
			Visibility:   store.VisibilityTeam,
			OwnerID:      pgUUID(p.ID),
			Tier:         store.MemoryTierEpisodic,
			Kind:         kind,
			Title:        title,
			Body:         row.Body,
			Identifiers:  []string{},
			Status:       store.MemoryStatusUnverified,
			Source:       src,
			Verification: ver,
			CreatedBy:    pgUUID(p.ID),
		}); err != nil {
			return fmt.Errorf("import apply: memory: %w", err)
		}
	}

	applyID := applyClientID(req)
	if err := q.InsertIngestReceipt(ctx, store.InsertIngestReceiptParams{
		ClientID:    pgUUID(applyID),
		PrincipalID: pgUUID(p.ID),
		SubjectType: "import",
		SubjectID:   pgUUID(applyID),
	}); err != nil {
		return fmt.Errorf("import apply: receipt: %w", err)
	}
	macID := machineClientID(req.Machine, req.Scope)
	_, err = q.GetIngestReceipt(ctx, pgUUID(macID))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := q.InsertIngestReceipt(ctx, store.InsertIngestReceiptParams{
			ClientID:    pgUUID(macID),
			PrincipalID: pgUUID(p.ID),
			SubjectType: "import_machine",
			SubjectID:   pgUUID(macID),
		}); err != nil {
			return fmt.Errorf("import apply: machine receipt: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("import apply: machine receipt: %w", err)
	}
	if req.Machine == req.TrustedMachine {
		if err := storeTrustedHost(ctx, q, p, req, leafID); err != nil {
			return err
		}
	}
	return nil
}

const trustedSubjectPrefix = "import_trusted:"

func trustedMarkerID(scope string) uuid.UUID {
	return uuid.NewSHA1(importNS, []byte("substrate-import-trusted/"+scope))
}

func loadTrustedHost(ctx context.Context, q *store.Queries, scope string) (string, error) {
	row, err := q.GetIngestReceipt(ctx, pgUUID(trustedMarkerID(scope)))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("import apply: trusted receipt: %w", err)
	}
	host, ok := strings.CutPrefix(row.SubjectType, trustedSubjectPrefix)
	if !ok || host == "" {
		return "", fmt.Errorf("import apply: trusted receipt: malformed subject_type %q", row.SubjectType)
	}
	return host, nil
}

func storeTrustedHost(ctx context.Context, q *store.Queries, p *identity.Principal, req ApplyRequest, leafID uuid.UUID) error {
	id := trustedMarkerID(req.Scope)
	_, err := q.GetIngestReceipt(ctx, pgUUID(id))
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("import apply: trusted receipt: %w", err)
	}
	if err := q.InsertIngestReceipt(ctx, store.InsertIngestReceiptParams{
		ClientID:    pgUUID(id),
		PrincipalID: pgUUID(p.ID),
		SubjectType: trustedSubjectPrefix + req.Machine,
		SubjectID:   pgUUID(leafID),
	}); err != nil {
		return fmt.Errorf("import apply: trusted receipt: %w", err)
	}
	return nil
}

func conflictPayload(req ApplyRequest, row PlannedRow) ([]byte, error) {
	var pair []ConflictSide
	for _, c := range req.Plan.Conflicts {
		if c.Slot == row.Slot {
			pair = c.Pair
			break
		}
	}
	return json.Marshal(map[string]any{
		"hostname": req.Machine,
		"slot":     row.Slot,
		"pair":     pair,
	})
}

func lookupPath(ctx context.Context, q *store.Queries, p scope.Path) (ids []pgtype.UUID, teamID, leaf uuid.UUID, err error) {
	var parent pgtype.UUID
	for i := range p {
		row, err := q.LookupScope(ctx, store.LookupScopeParams{
			Kind:     store.ScopeKind(p[i].Kind),
			Key:      p[i].Name,
			ParentID: parent,
		})
		if err != nil {
			return nil, uuid.Nil, uuid.Nil, fmt.Errorf("import apply: unknown scope %s: %w", p[:i+1].String(), err)
		}
		ids = append(ids, row.ID)
		id := uuid.UUID(row.ID.Bytes)
		leaf = id
		if p[i].Kind == scope.Team {
			teamID = id
		}
		parent = row.ID
	}
	if teamID == uuid.Nil {
		teamID = leaf
	}
	return ids, teamID, leaf, nil
}

func identicalActive(body string, ins []store.ListActiveInstructionsRow, pref []store.ListActivePreferencesRow) bool {
	for _, r := range ins {
		if r.Body == body {
			return true
		}
	}
	for _, r := range pref {
		if r.Body == body {
			return true
		}
	}
	return false
}

func blockOnMachine(b Block, machine string) bool {
	for _, s := range b.Sources {
		if s.Hostname == machine {
			return true
		}
	}
	return false
}

func hostIn(hosts []string, machine string) bool {
	for _, h := range hosts {
		if h == machine {
			return true
		}
	}
	return false
}

func applyClientID(req ApplyRequest) uuid.UUID {
	if req.ClientID != "" {
		if id, err := uuid.Parse(req.ClientID); err == nil {
			return id
		}
	}
	raw, err := json.Marshal(req.Plan)
	if err != nil {
		raw = []byte(req.Machine)
	}
	sum := sha256.Sum256(raw)
	return uuid.NewSHA1(importNS, []byte("substrate-import/"+req.Machine+"/"+req.Scope+"/"+fmt.Sprintf("%x", sum)))
}

func machineClientID(host, scope string) uuid.UUID {
	return uuid.NewSHA1(importNS, []byte("substrate-import-machine/"+host+"/"+scope))
}

func rowKey(kind, rel, heading, hash string) string {
	h := hash
	if len(h) > 12 {
		h = h[:12]
	}
	return strings.Join([]string{"import", slug(kind), slug(rel), slug(heading), h}, ".")
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
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

func validMemoryKind(s string) bool {
	switch store.MemoryKind(s) {
	case store.MemoryKindFact, store.MemoryKindDecision, store.MemoryKindIncident, store.MemoryKindLesson, store.MemoryKindObservation:
		return true
	default:
		return false
	}
}

func sortPlanned(res *ApplyResult) {
	less := func(a, b PlannedRow) bool {
		if a.Hash != b.Hash {
			return a.Hash < b.Hash
		}
		return a.Key < b.Key
	}
	sort.Slice(res.Active, func(i, j int) bool { return less(res.Active[i], res.Active[j]) })
	sort.Slice(res.Proposed, func(i, j int) bool { return less(res.Proposed[i], res.Proposed[j]) })
	sort.Slice(res.Conflict, func(i, j int) bool { return less(res.Conflict[i], res.Conflict[j]) })
	sort.Slice(res.Memory, func(i, j int) bool { return less(res.Memory[i], res.Memory[j]) })
}

func (r *ApplyResult) finalize() {
	if r == nil {
		return
	}
	r.ByHostname = map[string]HostSummary{}
	bump := func(row PlannedRow, field string) {
		s := r.ByHostname[row.Hostname]
		switch field {
		case "active":
			s.Active++
		case "proposed":
			s.Proposed++
		case "conflict":
			s.Conflict++
		case "memory":
			s.Memory++
		}
		r.ByHostname[row.Hostname] = s
	}
	for _, row := range r.Active {
		bump(row, "active")
	}
	for _, row := range r.Proposed {
		bump(row, "proposed")
	}
	for _, row := range r.Conflict {
		bump(row, "conflict")
	}
	for _, row := range r.Memory {
		bump(row, "memory")
	}
}

// Format prints planned writes grouped by hostname. Empty means nothing
// would change — dry-run tests require this to be non-empty when work exists.
func (r *ApplyResult) Format() string {
	if r == nil {
		return ""
	}
	if r.ByHostname == nil {
		r.finalize()
	}
	hosts := make([]string, 0, len(r.ByHostname))
	for h := range r.ByHostname {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	var b strings.Builder
	if r.DryRun {
		b.WriteString("dry-run\n")
	}
	if r.Duplicate {
		b.WriteString("duplicate\n")
	}
	for _, h := range hosts {
		s := r.ByHostname[h]
		fmt.Fprintf(&b, "%s: active=%d proposed=%d conflict=%d memory=%d\n", h, s.Active, s.Proposed, s.Conflict, s.Memory)
	}
	write := func(rows []PlannedRow) {
		for _, row := range rows {
			fmt.Fprintf(&b, "  %s %s %s %s %s\n", row.Hostname, row.Status, row.Kind, row.Key, strings.ReplaceAll(row.Body, "\n", " "))
		}
	}
	write(r.Active)
	write(r.Proposed)
	write(r.Conflict)
	write(r.Memory)
	return b.String()
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
