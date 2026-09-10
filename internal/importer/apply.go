package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	ids, teamID, leafID, _, err := lookupPath(ctx, q, sc)
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
	if alias, ok := aliasClientID(req); ok {
		_, aliasErr := q.GetIngestReceipt(ctx, pgUUID(alias))
		if aliasErr == nil {
			res := &ApplyResult{Duplicate: true}
			res.finalize()
			return res, nil
		}
		if !errors.Is(aliasErr, pgx.ErrNoRows) {
			return nil, fmt.Errorf("import apply: receipt: %w", aliasErr)
		}
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
	if err := validatePlanSlots(req.Plan); err != nil {
		return nil, err
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
	conflictSlots := map[string]bool{}
	for _, c := range req.Plan.Conflicts {
		conflictSlots[c.Slot] = true
		for _, side := range c.Pair {
			conflictHash[side.Hash] = c
		}
	}

	for _, b := range req.Plan.Blocks {
		if !blockOnMachine(b, req.Machine) {
			continue
		}
		if _, ok := conflictHash[b.Hash]; ok {
			continue
		}
		rel := sanitizeStored(b.Rel)
		heading := sanitizeStored(b.Heading)
		slot := rel + "#" + heading
		if conflictSlots[b.Rel+"#"+b.Heading] || conflictSlots[slot] {
			continue
		}
		body := sanitizeStored(b.Body)
		kind := blockKind(b)
		if match, ok := identicalActive(body, activeIns, activePref); ok {
			// Hash-only source: rel/heading have not been secret-scanned yet.
			identicalSkip(res, skipSourceHashOnly(kind, b.Hash), match)
			continue
		}
		// Location fields first: if they are the secret, Source must omit them.
		if secretSkip(res, skipSourceHashOnly(kind, b.Hash), rel, heading) {
			continue
		}
		machine := sanitizeStored(req.Machine)
		src := skipSource(kind, rel, heading, b.Hash)
		if secretSkip(res, src, body, machine) {
			continue
		}
		row := PlannedRow{
			Hostname: machine,
			Kind:     kind,
			Key:      rowKey(kind, rel, heading, b.Ordinal),
			Hash:     b.Hash,
			Body:     body,
		}
		if existing := activeBodyAtKey(row.Kind, row.Key, activeIns, activePref); existing != "" && existing != body {
			other := sanitizeStored(storedHost)
			if other == "" {
				other = machine
			}
			if secretSkip(res, src, other) {
				continue
			}
			row.Status = string(store.InstructionStatusProposed)
			row.Slot = slot
			res.Proposed = append(res.Proposed, row)
			if !reviewSlots[slot] {
				res.Conflict = append(res.Conflict, PlannedRow{
					Hostname: machine,
					Kind:     "import_conflict",
					Status:   "conflict",
					Hash:     b.Hash,
					Body:     body,
					Slot:     slot,
					Pair: []ConflictSide{
						// Both sides sit at the same ordinal by construction:
						// `existing` is the row holding row.Key.
						{Hash: sha256Hex([]byte(existing)), Hostnames: []string{other}, Body: existing, Ordinal: b.Ordinal},
						{Hash: b.Hash, Hostnames: []string{machine}, Body: body, Ordinal: b.Ordinal},
					},
				})
				reviewSlots[slot] = true
			}
			continue
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
		rel := sanitizeStored(relOfSlot(c.Slot))
		heading := sanitizeStored(headingOfSlot(c.Slot))
		slot := rel + "#" + heading
		for _, side := range c.Pair {
			if !hostIn(side.Hostnames, req.Machine) {
				continue
			}
			body := sanitizeStored(side.Body)
			kind := blockKind(Block{Heading: heading, Body: body, Rel: rel})
			if match, ok := identicalActive(body, activeIns, activePref); ok {
				identicalSkip(res, skipSourceHashOnly(kind, side.Hash), match)
				continue
			}
			if secretSkip(res, skipSourceHashOnly(kind, side.Hash), rel, heading) {
				continue
			}
			machine := sanitizeStored(req.Machine)
			src := skipSource(kind, rel, heading, side.Hash)
			if secretSkip(res, src, body, machine) {
				continue
			}
			pair := make([]ConflictSide, len(c.Pair))
			pairSecret := false
			for i, s := range c.Pair {
				sb := sanitizeStored(s.Body)
				hosts := make([]string, len(s.Hostnames))
				for j, h := range s.Hostnames {
					hosts[j] = sanitizeStored(h)
				}
				pair[i] = ConflictSide{
					Hash:      s.Hash,
					Hostnames: hosts,
					Body:      sb,
					Ordinal:   s.Ordinal,
				}
				sideSrc := skipSource(kind, rel, heading, s.Hash)
				parts := append([]string{sb}, hosts...)
				if secretSkip(res, sideSrc, parts...) {
					pairSecret = true
				}
			}
			if pairSecret {
				continue
			}
			res.Proposed = append(res.Proposed, PlannedRow{
				Hostname: machine,
				Kind:     kind,
				Status:   string(store.InstructionStatusProposed),
				Key:      rowKey(kind, rel, heading, side.Ordinal),
				Hash:     side.Hash,
				Body:     body,
				Slot:     slot,
			})
			if !reviewSlots[slot] && !reviewSlots[c.Slot] {
				res.Conflict = append(res.Conflict, PlannedRow{
					Hostname: machine,
					Kind:     "import_conflict",
					Status:   "conflict",
					Hash:     side.Hash,
					Body:     body,
					Slot:     slot,
					Pair:     pair,
				})
				reviewSlots[slot] = true
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
		body := sanitizeStored(m.Body)
		title := sanitizeStored(m.Title)
		if title == "" {
			title = body
		}
		// Machine is what lands in memory.source jsonb (parity with
		// memory.scanStored's machine field). Kind is not scanned: invalid
		// kinds are replaced with "observation" before insert.
		machine := sanitizeStored(req.Machine)
		if secretSkip(res, skipSourceHashOnly("memory", m.Hash), machine) {
			continue
		}
		src := skipSource("memory", "", "", m.Hash)
		if secretSkip(res, src, title, body) {
			continue
		}
		res.Memory = append(res.Memory, PlannedRow{
			Hostname: machine,
			Kind:     kind,
			Status:   string(store.MemoryStatusUnverified),
			Hash:     m.Hash,
			Body:     body,
			Title:    title,
		})
	}

	labelTargets(res, sc)
	sortPlanned(res)
	_ = teamID
	_ = leafID
	return res, nil
}

// labelTargets stamps every planned row with the scope and visibility the
// commit will use, so `import apply` without -commit already shows where each
// row lands (#86). commitWrites reads the same values back, which is what
// keeps the preview honest.
func labelTargets(res *ApplyResult, sc scope.Path) {
	stamp := func(rows []PlannedRow) {
		for i := range rows {
			path, vis := plannedTarget(sc, rows[i].Kind)
			rows[i].Scope = path
			rows[i].Visibility = string(vis)
		}
	}
	stamp(res.Active)
	stamp(res.Proposed)
	stamp(res.Conflict)
	stamp(res.Memory)
}

// plannedTarget is the single source of truth for where an imported row is
// filed. Preferences keep the team scope id — nulling scope.team_id would put
// them out of reach of review_apply_preference_status and
// ListPreferencesByBodies, both of which match on it — but they are filed
// owner-visible: a personal ~/.claude/CLAUDE.md must not become team-readable
// just because its owner onboarded (#86). A review item carries no
// visibility of its own, so it gets none here.
func plannedTarget(sc scope.Path, kind string) (string, store.Visibility) {
	if kind == "preference" {
		return teamPath(sc).String(), TargetVisibility(kind)
	}
	return sc.String(), TargetVisibility(kind)
}

// TargetVisibility is the visibility half of plannedTarget, exported because
// review decide files rows on the same terms when it flips a conflict's kind.
// Duplicating the literal there is how the two drift, and only one of the two
// is covered by the import tests.
func TargetVisibility(kind string) store.Visibility {
	switch kind {
	case "preference":
		return store.VisibilityOwner
	case "import_conflict":
		return ""
	default:
		return store.VisibilityTeam
	}
}

// teamPath mirrors lookupPath's teamScopeID choice, including its fallback to
// the leaf when the path has no team segment.
func teamPath(sc scope.Path) scope.Path {
	for i := range sc {
		if sc[i].Kind == scope.Team {
			return sc[:i+1]
		}
	}
	return sc
}

func commitWrites(ctx context.Context, tx pgx.Tx, p *identity.Principal, req ApplyRequest, sc scope.Path, res *ApplyResult) error {
	if res.Duplicate {
		return nil
	}
	q := store.New(tx)
	_, teamScopeID, leafID, leafTeam, err := lookupPath(ctx, q, sc)
	if err != nil {
		return err
	}
	teamID := leafTeam

	writeRow := func(row PlannedRow) error {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		// Use the visibility the dry-run already showed the operator, so the
		// preview cannot drift from the write. labelTargets stamps every
		// Active and Proposed row and those are the only kinds reaching
		// writeRow, so the fallback below is unreachable today: it is
		// defensive against a future planner path that forgets to label,
		// not a live branch.
		vis := store.Visibility(row.Visibility)
		if vis == "" {
			_, vis = plannedTarget(sc, row.Kind)
		}
		switch row.Kind {
		case "preference":
			_, err = q.InsertPreference(ctx, store.InsertPreferenceParams{
				ID:         pgUUID(id),
				ScopeID:    pgUUID(teamScopeID),
				Visibility: vis,
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
				Visibility: vis,
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
		payload, err := conflictPayload(row)
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
	if alias, ok := aliasClientID(req); ok && alias != applyID {
		if err := q.InsertIngestReceipt(ctx, store.InsertIngestReceiptParams{
			ClientID:    pgUUID(alias),
			PrincipalID: pgUUID(p.ID),
			SubjectType: "import_alias",
			SubjectID:   pgUUID(applyID),
		}); err != nil {
			return fmt.Errorf("import apply: receipt alias: %w", err)
		}
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

func conflictPayload(row PlannedRow) ([]byte, error) {
	pair := row.Pair
	if len(pair) == 0 {
		return nil, fmt.Errorf("import apply: conflict slot %q missing pair", row.Slot)
	}
	hosts := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, side := range pair {
		if len(side.Hostnames) == 0 {
			return nil, fmt.Errorf("import apply: conflict slot %q has a side with empty hostname", row.Slot)
		}
		for _, h := range side.Hostnames {
			if strings.TrimSpace(h) == "" {
				return nil, fmt.Errorf("import apply: conflict slot %q has a side with empty hostname", row.Slot)
			}
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}
	sort.Strings(hosts)
	return json.Marshal(map[string]any{
		"hostname":  row.Hostname,
		"hostnames": hosts,
		"slot":      row.Slot,
		"pair":      pair,
	})
}

func validatePlanSlots(plan Plan) error {
	inConflict := map[string]bool{}
	for _, c := range plan.Conflicts {
		for _, side := range c.Pair {
			if len(side.Hostnames) == 0 {
				return fmt.Errorf("import apply: conflict slot %q has a side with empty hostname", c.Slot)
			}
			for _, h := range side.Hostnames {
				if strings.TrimSpace(h) == "" {
					return fmt.Errorf("import apply: conflict slot %q has a side with empty hostname", c.Slot)
				}
			}
			inConflict[side.Hash] = true
		}
	}
	// Keyed by ordinal, not by slot alone: splitBlocks emits one block per
	// bullet, so a heading with N bullets legitimately holds N distinct hashes
	// on a single machine. Keying on the slot refused every such plan — which
	// is to say almost every real file (#93). What the guard is actually for is
	// two different bodies claiming one identity, and identity is the ordinal.
	//
	// Identity here MUST be the same rowKey that will actually be written
	// (#96): rowKey slugs kind/rel/heading before joining them, so two blocks
	// whose raw Rel or Heading differ only in characters slug() strips (case,
	// punctuation) land on one row downstream even though their raw strings
	// look distinct. Comparing raw strings here let that pair sail through the
	// guard and then collide on the actual key. Computing rowKey itself, not a
	// second hand-rolled normalization, keeps identity defined in one place.
	type slotSeen struct {
		hash string
		raw  string
	}
	seen := map[string]slotSeen{}
	for _, b := range plan.Blocks {
		key := rowKey(blockKind(b), b.Rel, b.Heading, b.Ordinal)
		raw := b.Rel + "#" + b.Heading
		prev, ok := seen[key]
		if !ok {
			seen[key] = slotSeen{hash: b.Hash, raw: raw}
			continue
		}
		if prev.hash == b.Hash {
			continue
		}
		if !inConflict[prev.hash] || !inConflict[b.Hash] {
			return fmt.Errorf("import apply: slots %q and %q ordinal %d compute the same key %q with distinct hashes not listed in conflicts", prev.raw, raw, b.Ordinal, key)
		}
	}
	return nil
}

// activeBodyAtKey returns the body of the active row that already holds this
// exact key, or "" if the key is free. It matches the whole key, not the slot
// prefix: under ordinal keys a slot is a namespace holding one row per bullet,
// so only the row at the same ordinal is the incoming block's predecessor. A
// prefix match pairs an edit against an arbitrary sibling, attaching the
// supersession edge to a rule the edit never touched (Gotcha 6), and reports a
// brand-new bullet as a conflict with a rule it does not contradict.
func activeBodyAtKey(kind, key string, ins []store.ListActiveInstructionsRow, pref []store.ListActivePreferencesRow) string {
	if kind == "preference" {
		for _, r := range pref {
			if r.Key == key {
				return r.Body
			}
		}
		return ""
	}
	for _, r := range ins {
		if r.Key == key {
			return r.Body
		}
	}
	return ""
}

// sanitizeStored strips control bytes and caps length so the scanner sees the
// bytes that land in the row (same order as memory.prepareWrite).
func sanitizeStored(s string) string {
	return capBody(stripControls(s))
}

func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func capBody(s string) string {
	if len(s) <= 4096 {
		return s
	}
	s = s[:4096]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func truncHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// skipSource builds a locatable skip id from already-scanned-clean location
// fields plus a hash disambiguator. Do not pass fields that failed ScanSecrets —
// use skipSourceHashOnly when the location itself is the secret.
func skipSource(kind, rel, heading, hash string) string {
	h := truncHash(hash)
	if rel == "" {
		return kind + ":" + h
	}
	if heading == "" {
		return kind + ":" + rel + ":" + h
	}
	return kind + ":" + rel + "#" + heading + ":" + h
}

func skipSourceHashOnly(kind, hash string) string {
	return kind + ":" + truncHash(hash)
}

// identicalSkip records a block dropped because its body already sits active in
// scope. Same shape as secretSkip: the drop has to leave a trace naming both the
// block and what it matched, or an operator sees only a smaller result.
func identicalSkip(res *ApplyResult, source, match string) {
	res.Skipped = append(res.Skipped, Skipped{
		Source: source,
		Reason: "identical to active " + match,
	})
}

func secretSkip(res *ApplyResult, source string, parts ...string) bool {
	for _, p := range parts {
		if err := policy.ScanSecrets(p); err != nil {
			res.Skipped = append(res.Skipped, Skipped{
				Source: source,
				Reason: policy.CodeSecretDetected,
			})
			return true
		}
	}
	return false
}

func lookupPath(ctx context.Context, q *store.Queries, p scope.Path) (ids []pgtype.UUID, teamScopeID, leaf, leafTeam uuid.UUID, err error) {
	var parent pgtype.UUID
	for i := range p {
		row, err := q.LookupScope(ctx, store.LookupScopeParams{
			Kind:     store.ScopeKind(p[i].Kind),
			Key:      p[i].Name,
			ParentID: parent,
		})
		if err != nil {
			return nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("import apply: unknown scope %s: %w", p[:i+1].String(), err)
		}
		ids = append(ids, row.ID)
		id := uuid.UUID(row.ID.Bytes)
		leaf = id
		if row.TeamID.Valid {
			leafTeam = uuid.UUID(row.TeamID.Bytes)
		}
		if p[i].Kind == scope.Team {
			teamScopeID = id
		}
		parent = row.ID
	}
	if teamScopeID == uuid.Nil {
		teamScopeID = leaf
	}
	return ids, teamScopeID, leaf, leafTeam, nil
}

// identicalActive reports whether this body already sits active anywhere in the
// scope chain, and names the row it matched. The match is on body text alone,
// deliberately: narrowing it to the key would change which blocks are admitted.
// The name is what makes the resulting drop auditable — a skipped block with no
// record of what displaced it is Gotcha 6's silent shrink.
func identicalActive(body string, ins []store.ListActiveInstructionsRow, pref []store.ListActivePreferencesRow) (string, bool) {
	for _, r := range ins {
		if r.Body == body {
			return "instruction " + r.Key, true
		}
	}
	for _, r := range pref {
		if r.Body == body {
			return "preference " + r.Key, true
		}
	}
	return "", false
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
	raw, err := json.Marshal(req.Plan)
	if err != nil {
		raw = []byte(req.Machine)
	}
	sum := sha256.Sum256(raw)
	return uuid.NewSHA1(importNS, []byte("substrate-import/"+req.Machine+"/"+req.Scope+"/"+fmt.Sprintf("%x", sum)))
}

func aliasClientID(req ApplyRequest) (uuid.UUID, bool) {
	if req.ClientID == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(req.ClientID)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func machineClientID(host, scope string) uuid.UUID {
	return uuid.NewSHA1(importNS, []byte("substrate-import-machine/"+host+"/"+scope))
}

// blockKind is the one place that derives a row's kind when the block did not
// arrive with one already classified. Both the write path (planWrites) and the
// slot-identity guard (validatePlanSlots) call this — and only this — before
// handing kind to rowKey, so the two routes cannot drift on what "kind" means
// for a given block (#96).
func blockKind(b Block) string {
	if b.Kind != "" {
		return b.Kind
	}
	return Classify(b).Kind
}

// rowKey keys an imported row by where it came from, not by what it says: a
// content hash made every edit a brand-new rule that could override nothing
// (#80). The ordinal is the bullet's position in its slot.
//
// Ordinal, like the hash before it, arrives in an operator-supplied plan.json
// and is never checked against real source position, so the key is not a trust
// boundary. What stops a forged or colliding key from promoting the wrong
// content is the body-equality check in activeBodyAtKey/identicalActive plus
// the hash-parity guard in validatePlanSlots — not key uniqueness. Do not
// simplify either away on the grounds that the key already discriminates.
//
// Accepted limitation: a pure reorder of unedited bullets is skipped by
// identicalActive before its key is ever consulted, so those rows keep a
// now-stale ordinal. Reconciling them is deliberately not attempted.
func rowKey(kind, rel, heading string, ordinal int) string {
	return strings.Join([]string{"import", slug(kind), slug(rel), slug(heading), strconv.Itoa(ordinal)}, ".")
}

// RowKey exposes rowKey to review decide, which re-keys a row whose kind an
// operator flipped. It must land on the key a re-import would compute, or the
// flipped row is an orphan no later import can match (#80).
func RowKey(kind, rel, heading string, ordinal int) string {
	return rowKey(kind, rel, heading, ordinal)
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
			target := ""
			if row.Scope != "" {
				target += " scope=" + row.Scope
			}
			if row.Visibility != "" {
				target += " visibility=" + row.Visibility
			}
			fmt.Fprintf(&b, "  %s %s %s%s %s %s\n", row.Hostname, row.Status, row.Kind, target, row.Key, strings.ReplaceAll(row.Body, "\n", " "))
		}
	}
	write(r.Active)
	write(r.Proposed)
	write(r.Conflict)
	write(r.Memory)
	for _, s := range r.Skipped {
		fmt.Fprintf(&b, "  skipped %s %s\n", s.Source, s.Reason)
	}
	return b.String()
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
