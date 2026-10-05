package artifacts

import (
	"context"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func (o *Owner) Approve(a Approval) (Receipt, error) {
	ctx, err := o.store.authority.OwnerContext(o.checkout)
	if err != nil {
		return Receipt{}, err
	}
	if !validText(a.OperationID, a.ArtifactID, a.RevisionID, a.ExpectedRevision, a.Overrides, a.OverrideRevision) {
		return Receipt{}, ErrInvalidText
	}
	if !operationID(a.OperationID) || (a.Overrides == "") != (a.OverrideRevision == "") {
		return Receipt{}, ErrConflict
	}
	hash := fingerprint(struct {
		Action string
		Approval
	}{"approve", a})
	tx, err := o.store.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	defer tx.Rollback()
	if r, found, err := retry(tx, ctx, a.OperationID, hash); err != nil || found {
		return r, err
	}
	artifact, err := lookup(tx, ctx, a.ArtifactID)
	if err != nil {
		return Receipt{}, err
	}
	all, err := selections(tx, ctx)
	if err != nil {
		return Receipt{}, err
	}
	var selected *selection
	for i := range all {
		if all[i].ArtifactID == a.ArtifactID {
			selected = &all[i]
		}
	}
	if selected == nil || artifact.Kind == "memory" {
		return Receipt{}, authority.ErrDenied
	}
	if artifact.Lifecycle != "active" || artifact.HeadRevision != a.ExpectedRevision {
		return Receipt{}, ErrConflict
	}
	r, err := loadRevision(tx, a.ArtifactID, a.RevisionID)
	if err != nil {
		return Receipt{}, err
	}
	eligible := r.State == "candidate" && r.Base == a.ExpectedRevision
	if a.ResolveConflict {
		eligible = r.State == "candidate" || r.State == "conflict"
	}
	if r.Source == nil || !eligible {
		return Receipt{}, ErrConflict
	}
	// One-level relationships keep cycles and implicit override chains unavailable.
	if a.Overrides != "" {
		var target *selection
		for i := range all {
			if all[i].ArtifactID == a.Overrides {
				target = &all[i]
			}
		}
		if target == nil {
			return Receipt{}, authority.ErrDenied
		}
		if target.ArtifactID == a.ArtifactID || target.lifecycle != "active" || target.RevisionID == "" || target.RevisionID != a.OverrideRevision || !target.Overridable || target.kind != selected.kind || target.Overrides != "" || selected.Overridable || target.Alias == "" || target.Alias != selected.Alias {
			return Receipt{}, ErrConflict
		}
	}
	revision := a.RevisionID
	if a.ResolveConflict {
		revision, err = cloneRevision(tx, ctx, artifact, r, "candidate")
		if err != nil {
			return Receipt{}, err
		}
		if _, err := tx.Exec("UPDATE revisions SET state='resolved' WHERE artifact_id=? AND state IN ('candidate','conflict') AND id!=?", a.ArtifactID, revision); err != nil {
			return Receipt{}, ErrUnavailable
		}
	}
	if _, err := tx.Exec("INSERT INTO approvals VALUES(?,?,?)", revision, a.Overrides, a.OverrideRevision); err != nil {
		return Receipt{}, ErrUnavailable
	}
	if _, err := tx.Exec("UPDATE revisions SET state='approved' WHERE id=?", revision); err != nil {
		return Receipt{}, ErrUnavailable
	}
	if _, err := tx.Exec("UPDATE artifacts SET head=? WHERE id=?", revision, a.ArtifactID); err != nil {
		return Receipt{}, ErrUnavailable
	}
	if _, err := tx.Exec("UPDATE revisions SET state='conflict' WHERE artifact_id=? AND state='candidate' AND base!=?", a.ArtifactID, revision); err != nil {
		return Receipt{}, ErrUnavailable
	}
	receipt := Receipt{OperationID: a.OperationID, ArtifactID: a.ArtifactID, RevisionID: revision, State: "approved"}
	if err := record(tx, ctx, hash, receipt, "approve", artifact.HeadRevision != revision); err != nil {
		return Receipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Receipt{}, ErrUnavailable
	}
	return receipt, nil
}
