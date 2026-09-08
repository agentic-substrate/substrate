package rest

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/agentic-substrate/substrate/internal/importer"
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
	ClientID       string        `json:"client_id"`
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
		writePolicy(w, maskRepoDenial(err, in.Repo != ""))
		return
	}
	if in.Repo != "" {
		// labelTargets stamps each planned row with the chain it will be filed
		// at (#86). A caller who named a repo key never learns that chain --
		// repoScope reads the RLS-free `scope` table, so echoing it back here
		// would disclose exactly the org/team/project naming the key exists to
		// keep private. A scope-path caller already knows it and keeps it.
		for _, rows := range [][]importer.PlannedRow{res.Active, res.Proposed, res.Conflict, res.Memory} {
			for i := range rows {
				rows[i].Scope = ""
			}
		}
	}
	code := http.StatusOK
	if commit && !res.Duplicate {
		code = http.StatusCreated
	}
	writeJSON(w, code, res)
}
