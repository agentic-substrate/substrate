package artifacts

import (
	"context"
	"database/sql"
	"errors"

	"github.com/agentic-substrate/substrate/internal/authority"
)

const publicationSchema = `
CREATE TABLE publication_policy(owner_id TEXT NOT NULL, scope TEXT NOT NULL, scope_id TEXT NOT NULL,
 action TEXT NOT NULL, allowed INTEGER NOT NULL CHECK(allowed IN (0,1)), epoch INTEGER NOT NULL,
 PRIMARY KEY(owner_id,scope,scope_id,action));
CREATE TABLE publication_proposals(id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, space_id TEXT NOT NULL,
 repo_id TEXT NOT NULL, head TEXT NOT NULL, receipt TEXT NOT NULL, review TEXT NOT NULL);
CREATE TABLE publication_revisions(id TEXT PRIMARY KEY, proposal_id TEXT NOT NULL REFERENCES publication_proposals(id),
 owner_id TEXT NOT NULL, space_id TEXT NOT NULL, repo_id TEXT NOT NULL, operation_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(owner_id,space_id,repo_id,operation_id));
CREATE TABLE review_grants(token_hash TEXT PRIMARY KEY, snapshot TEXT NOT NULL, expires_at INTEGER NOT NULL,
 operation_id TEXT NOT NULL, receipt TEXT NOT NULL);
PRAGMA user_version=4;
`

func publicationPolicy(tx *sql.Tx, ctx authority.Context, scope, id, action string) (PublicationPolicy, error) {
	p := PublicationPolicy{Scope: scope, ID: id, Action: action, Allowed: scope == "artifact"}
	err := tx.QueryRow("SELECT allowed,epoch FROM publication_policy WHERE owner_id=? AND scope=? AND scope_id=? AND action=?", ctx.OwnerID, scope, id, action).Scan(&p.Allowed, &p.Epoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PublicationPolicy{}, ErrUnavailable
	}
	return p, nil
}

func (o *Owner) SetPublicationPolicy(action, artifact string, allowed bool) error {
	if !validText(action, artifact) {
		return ErrInvalidText
	}
	if action != "export" && action != "publish" || artifact != "" && action != "export" {
		return errors.New("publication policy requires export or publish; artifact restrictions apply only to export")
	}
	return o.store.authority.WithOwnerContexts([]string{o.checkout}, func(contexts []authority.Context) error {
		ctx := contexts[0]
		tx, err := o.store.db.BeginTx(context.Background(), nil)
		if err != nil {
			return ErrUnavailable
		}
		defer tx.Rollback()
		scope, id := "space", ctx.SpaceID
		if artifact != "" {
			if _, err := lookup(tx, ctx, artifact); err != nil {
				return err
			}
			scope, id = "artifact", artifact
		}
		if _, err := tx.Exec(`INSERT INTO publication_policy VALUES(?,?,?,?,?,1)
 ON CONFLICT(owner_id,scope,scope_id,action) DO UPDATE SET allowed=excluded.allowed,epoch=epoch+1`, ctx.OwnerID, scope, id, action, allowed); err != nil {
			return ErrUnavailable
		}
		if tx.Commit() != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func sourceSnapshot(tx *sql.Tx, ctx authority.Context, refs []PublicationSource) ([]PublicationSourceSnapshot, []PublicationPolicy, error) {
	space, err := publicationPolicy(tx, ctx, "space", ctx.SpaceID, "export")
	if err != nil {
		return nil, nil, err
	}
	policies := []PublicationPolicy{space}
	all, err := eligible(tx, ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]current{}
	for _, c := range all {
		byID[c.id] = c
	}
	snapshots := make([]PublicationSourceSnapshot, 0, len(refs))
	denied := !space.Allowed
	for _, ref := range refs {
		if _, err := lookup(tx, ctx, ref.ArtifactID); err != nil {
			return nil, nil, err
		}
		policy, err := publicationPolicy(tx, ctx, "artifact", ref.ArtifactID, "export")
		if err != nil {
			return nil, nil, err
		}
		policies = append(policies, policy)
		denied = denied || !policy.Allowed
		c, ok := byID[ref.ArtifactID]
		if !ok {
			return nil, nil, authority.ErrDenied
		}
		if c.revision != ref.RevisionID || c.choice.State == "conflict" {
			if denied {
				return nil, nil, authority.ErrDenied
			}
			return nil, nil, ErrConflict
		}
		r, err := loadRevision(tx, c.id, c.revision)
		if err != nil {
			return nil, nil, err
		}
		if c.kind != "memory" && (r.State != "approved" || r.Source == nil) {
			return nil, nil, authority.ErrDenied
		}
		choice := c.choice
		if c.kind == "memory" {
			choice = Choice{ArtifactID: c.id, RevisionID: c.revision, State: r.State, Reason: "current permitted unverified observation"}
		}
		snapshots = append(snapshots, PublicationSourceSnapshot{ArtifactID: c.id, Kind: c.kind, Revision: r, Choice: choice})
	}
	if denied {
		return nil, nil, authority.ErrDenied
	}
	return snapshots, policies, nil
}
