package rest

import (
	"errors"
	"net/http"

	"github.com/agentic-substrate/substrate/internal/importer"
	"github.com/agentic-substrate/substrate/internal/store"
)

func (h *Handler) importApply(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	var in struct {
		Machine        string        `json:"machine"`
		TrustedMachine string        `json:"trusted_machine"`
		Scope          string        `json:"scope"`
		DryRun         bool          `json:"dry_run"`
		Commit         *bool         `json:"commit"`
		Plan           importer.Plan `json:"plan"`
		ClientID       string        `json:"client_id"`
	}
	if err := decodeJSONBody(w, r, &in); err != nil {
		writeDecodeErr(w, err)
		return
	}
	commit := in.Commit != nil && *in.Commit && !in.DryRun
	res, err := importer.Apply(r.Context(), st, importer.ApplyRequest{
		Plan:           in.Plan,
		Machine:        in.Machine,
		TrustedMachine: in.TrustedMachine,
		Scope:          in.Scope,
		Commit:         commit,
		ClientID:       in.ClientID,
	})
	if err != nil {
		if errors.Is(err, store.ErrNoPrincipal) {
			writeErr(w, http.StatusUnauthorized, err)
			return
		}
		writePolicy(w, err)
		return
	}
	code := http.StatusOK
	if commit && !res.Duplicate {
		code = http.StatusCreated
	}
	writeJSON(w, code, res)
}
