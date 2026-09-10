package rest

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/agentic-substrate/substrate/internal/importer"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// importApplyRequest is named, unlike its sibling handlers' anonymous bodies,
// so a test can pin Commit's type. A plain bool cannot express "the client
// omitted commit", and the wire protocol keeps that distinction: nil means
// dry-run, so an apply that forgets the flag previews instead of writing.
type importApplyRequest struct {
	Machine        string        `json:"machine"`
	TrustedMachine string        `json:"trusted_machine"`
	Scope          string        `json:"scope"`
	Repo           string        `json:"repo"`
	DryRun         bool          `json:"dry_run"`
	Commit         *bool         `json:"commit"`
	Plan           importer.Plan `json:"plan"`
	// Witness is what `import scan` recorded, derived by the caller from
	// inventory.json and never from the plan (#95). The server has no view of
	// the operator's disk, so this is the only thing that can tell a block the
	// scanner saw from one an editor of plan.json added; an apply without it
	// is refused rather than trusted.
	Witness  importer.InventoryWitness `json:"inventory_witness"`
	ClientID string                    `json:"client_id"`
}

func (h *Handler) importApply(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	var in importApplyRequest
	if err := decodeJSONBody(w, r, &in); err != nil {
		writeDecodeErr(w, err)
		return
	}
	// An import of a checkout names the repo key and nothing else; the server
	// owns the binding from a key to its chain (#58, R18). `scope` and `repo`
	// are mutually exclusive for the same reason POST /v1/review makes them so:
	// accepting both would let a caller name a repo and still choose the chain.
	if in.Repo != "" && in.Scope != "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("scope and repo are mutually exclusive"))
		return
	}
	if in.Repo == "" && in.Scope == "" {
		// Without either, scope.Parse fails deep inside importer.Apply and the
		// wrapped error falls through writePolicy's default as a 500 -- a
		// client mistake reported as a server fault, which also pollutes any
		// 5xx error budget. POST /v1/review answers 400 here; so does this.
		writeErr(w, http.StatusBadRequest, fmt.Errorf("scope: empty path"))
		return
	}
	scopePath := in.Scope
	if in.Repo != "" {
		sc, err := h.repoScope(r.Context(), st, in.Repo)
		if err != nil {
			writePolicy(w, maskRepoDenial(err, true))
			return
		}
		scopePath = sc.String()
	}
	commit := in.Commit != nil && *in.Commit && !in.DryRun
	res, err := importer.Apply(r.Context(), st, importer.ApplyRequest{
		Plan:           in.Plan,
		Witness:        in.Witness,
		Machine:        in.Machine,
		TrustedMachine: in.TrustedMachine,
		Scope:          scopePath,
		Commit:         commit,
		ClientID:       in.ClientID,
	})
	if err != nil {
		if errors.Is(err, store.ErrNoPrincipal) {
			writeErr(w, http.StatusUnauthorized, err)
			return
		}
		// A plan the inventory does not account for (#95) is a caller mistake,
		// not a server fault: answering 500 would report it as one and spend
		// the 5xx error budget on it, the same reasoning as the missing-scope
		// 400 above.
		if errors.Is(err, importer.ErrPlanNotWitnessed) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writePolicy(w, maskRepoDenial(err, in.Repo != ""))
		return
	}
	// labelTargets stamps each planned row with the full chain it will be
	// filed at (#86); the response returns only that chain's leaf kind (#108).
	// A repo-keyed caller must never learn the chain -- repoScope reads the
	// RLS-free `scope` table, so echoing it would disclose exactly the
	// org/team/project naming the key exists to keep private -- but blanking the
	// field outright deleted the placement preview from the path most traffic
	// now takes. The kind answers what a preview is for ("does this land at my
	// checkout, or at a team default?") and discloses no naming. The assumption
	// this rests on: the kind still reveals the depth at which the server bound
	// the key, which is uniform across repos today. If keys ever bind at mixed
	// depths, that depth becomes a signal about the caller's own key and this
	// substitution has to be revisited.
	//
	// Scope-path callers get the same field rather than the full label, so there
	// is one response shape: they supplied the chain, and every target is a
	// prefix of it, so truncating their own argument to this kind reconstructs
	// the label without the server repeating it.
	for _, rows := range [][]importer.PlannedRow{res.Active, res.Proposed, res.Conflict, res.Memory} {
		for i := range rows {
			rows[i].Scope = leafKind(rows[i].Scope)
		}
	}
	code := http.StatusOK
	if commit && !res.Duplicate {
		code = http.StatusCreated
	}
	writeJSON(w, code, res)
}

// leafKind reduces a stamped chain to the kind of its most specific segment.
// An unparseable path yields "" rather than a guess: `scope,omitempty` then
// drops the field, which is the pre-#86 shape and never a partial chain.
func leafKind(path string) string {
	p, err := scope.Parse(path)
	if err != nil || len(p) == 0 {
		return ""
	}
	return string(p[len(p)-1].Kind)
}
