package artifacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func lookup(tx *sql.Tx, ctx authority.Context, id string) (Artifact, error) {
	a := Artifact{ID: id, SpaceID: ctx.SpaceID, RepositoryID: ctx.RepositoryID}
	err := tx.QueryRow("SELECT kind, lifecycle, head FROM artifacts WHERE id=? AND owner_id=? AND space_id=? AND repo_id=?", id, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID).Scan(&a.Kind, &a.Lifecycle, &a.HeadRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, authority.ErrDenied
	}
	if err != nil {
		return Artifact{}, ErrUnavailable
	}
	return a, nil
}

func (s *Session) Inspect(id string) (Artifact, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Artifact{}, err
	}
	if !validText(id) {
		return Artifact{}, ErrInvalidText
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Artifact{}, ErrUnavailable
	}
	defer tx.Rollback()
	a, err := lookup(tx, ctx, id)
	if err != nil {
		return Artifact{}, err
	}
	rows, err := tx.Query("SELECT id, base, content, provenance, author_id, state, verification, source FROM revisions WHERE artifact_id=? ORDER BY rowid", id)
	if err != nil {
		return Artifact{}, ErrUnavailable
	}
	defer rows.Close()
	a.Revisions = []Revision{}
	for rows.Next() {
		var r Revision
		var source string
		if err := rows.Scan(&r.ID, &r.Base, &r.Content, &r.Provenance, &r.AuthorID, &r.State, &r.Verification, &source); err != nil {
			return Artifact{}, ErrUnavailable
		}
		if source != "" {
			if err := json.Unmarshal([]byte(source), &r.Source); err != nil {
				return Artifact{}, ErrUnavailable
			}
		}
		a.Revisions = append(a.Revisions, r)
	}
	if err := rows.Err(); err != nil {
		return Artifact{}, ErrUnavailable
	}
	return a, nil
}

func (s *Session) Pending() ([]Work, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return nil, err
	}
	rows, err := s.store.db.Query("SELECT operation_id, artifact_id, revision_id, action FROM pending WHERE owner_id=? AND space_id=? AND repo_id=? ORDER BY rowid", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	work := []Work{}
	for rows.Next() {
		var w Work
		if err := rows.Scan(&w.OperationID, &w.ArtifactID, &w.RevisionID, &w.Action); err != nil {
			return nil, ErrUnavailable
		}
		work = append(work, w)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrUnavailable
	}
	return work, nil
}
