package artifacts

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func (s *Session) ResolveConflict(a Resolution) (Receipt, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Receipt{}, err
	}
	if !validText(a.OperationID, a.ArtifactID, a.RevisionID, a.ExpectedRevision) {
		return Receipt{}, ErrInvalidText
	}
	if !operationID(a.OperationID) {
		return Receipt{}, ErrConflict
	}
	hash := fingerprint(struct {
		Action string
		Resolution
	}{"resolve-conflict", a})
	tx, err := s.store.db.BeginTx(context.Background(), nil)
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
	if artifact.Kind != "memory" {
		return Receipt{}, authority.ErrDenied
	}
	if artifact.Lifecycle != "active" || artifact.HeadRevision != a.ExpectedRevision {
		return Receipt{}, ErrConflict
	}
	r, err := loadRevision(tx, a.ArtifactID, a.RevisionID)
	if err != nil {
		return Receipt{}, err
	}
	id, err := cloneRevision(tx, ctx, artifact, r, "pending-local")
	if err != nil {
		return Receipt{}, err
	}
	if _, err := tx.Exec("UPDATE artifacts SET head=? WHERE id=?", id, artifact.ID); err != nil {
		return Receipt{}, ErrUnavailable
	}
	if _, err := tx.Exec("UPDATE revisions SET state='resolved' WHERE artifact_id=? AND state IN ('conflict','conflict-retired')", artifact.ID); err != nil {
		return Receipt{}, ErrUnavailable
	}
	receipt := Receipt{OperationID: a.OperationID, ArtifactID: artifact.ID, RevisionID: id, State: "pending-local"}
	if err := record(tx, ctx, hash, receipt, "resolve-conflict", artifact.HeadRevision != id); err != nil {
		return Receipt{}, err
	}
	if tx.Commit() != nil {
		return Receipt{}, ErrUnavailable
	}
	return receipt, nil
}

func (o *Owner) Restore(id, expected, operation string) (Receipt, error) {
	ctx, err := o.store.authority.OwnerContext(o.checkout)
	if err != nil {
		return Receipt{}, err
	}
	if !validText(id, expected, operation) {
		return Receipt{}, ErrInvalidText
	}
	if !operationID(operation) {
		return Receipt{}, ErrConflict
	}
	hash := fingerprint(struct{ Action, ID, Expected string }{"restore", id, expected})
	tx, err := o.store.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	defer tx.Rollback()
	if r, found, err := retry(tx, ctx, operation, hash); err != nil || found {
		return r, err
	}
	a, err := lookup(tx, ctx, id)
	if err != nil {
		return Receipt{}, err
	}
	if a.Lifecycle != "retired" || a.HeadRevision != expected {
		return Receipt{}, ErrConflict
	}
	oldHead := a.HeadRevision
	r := Receipt{OperationID: operation, ArtifactID: id, State: "candidate"}
	head := ""
	if a.Kind != "memory" {
		if _, err := tx.Exec("UPDATE revisions SET state='retired-candidate' WHERE artifact_id=? AND state IN ('candidate','conflict','conflict-retired')", id); err != nil {
			return Receipt{}, ErrUnavailable
		}
	}
	if a.HeadRevision == "" {
		r.State = "restored-awaiting-candidate"
	} else {
		selected, err := loadRevision(tx, id, a.HeadRevision)
		if err != nil {
			return Receipt{}, err
		}
		if a.Kind == "memory" {
			r.State = "pending-local"
		} else {
			a.HeadRevision = ""
		}
		r.RevisionID, err = cloneRevision(tx, ctx, a, selected, r.State)
		if err != nil {
			return Receipt{}, err
		}
		if a.Kind == "memory" {
			head = r.RevisionID
		}
	}
	if _, err := tx.Exec("UPDATE artifacts SET lifecycle='active', head=? WHERE id=?", head, id); err != nil {
		return Receipt{}, ErrUnavailable
	}
	if err := record(tx, ctx, hash, r, "restore", oldHead != head); err != nil {
		return Receipt{}, err
	}
	if tx.Commit() != nil {
		return Receipt{}, ErrUnavailable
	}
	return r, nil
}

func cloneRevision(tx *sql.Tx, ctx authority.Context, artifact Artifact, selected Revision, state string) (string, error) {
	id := identifier()
	var source string
	if selected.Source != nil {
		data, _ := json.Marshal(selected.Source)
		source = string(data)
	}
	if _, err := tx.Exec("INSERT INTO revisions VALUES(?,?,?,?,?,?,?,?,?)", id, artifact.ID, artifact.HeadRevision, selected.Content, selected.Provenance, ctx.OwnerID, state, "unverified", source); err != nil {
		return "", ErrUnavailable
	}
	if err := saveAssociations(tx, ctx, id, selected.Associations); err != nil {
		return "", err
	}
	return id, nil
}
