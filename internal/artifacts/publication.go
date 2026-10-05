package artifacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func validatePublication(p PublicationInput) error {
	if !validText(p.OperationID, p.ID, p.ExpectedRevision, p.Content, p.Destination.SpaceID, p.Destination.RepositoryID) {
		return ErrInvalidText
	}
	if !operationID(p.OperationID) || strings.TrimSpace(p.Content) == "" || len(p.Content) > 1024*1024 || len(p.Sources) < 1 || len(p.Sources) > 32 || len(p.ID) > 128 || len(p.ExpectedRevision) > 128 || p.ID == "" && p.ExpectedRevision != "" || len(p.Destination.SpaceID) != 64 || len(p.Destination.RepositoryID) != 64 {
		return errors.New("publication requires bounded lesson text, operation ID, exact source revisions, and destination IDs")
	}
	seen := map[string]bool{}
	for _, ref := range p.Sources {
		if !validText(ref.ArtifactID, ref.RevisionID) {
			return ErrInvalidText
		}
		if len(ref.ArtifactID) != 64 || len(ref.RevisionID) != 64 || seen[ref.ArtifactID] {
			return errors.New("publication sources require distinct exact artifact and revision IDs")
		}
		seen[ref.ArtifactID] = true
	}
	return nil
}

func lookupPublication(tx *sql.Tx, ctx authority.Context, id string) (Publication, error) {
	var payload, receipt string
	err := tx.QueryRow(`SELECT r.payload,p.receipt FROM publication_proposals p JOIN publication_revisions r ON r.id=p.head
 WHERE p.id=? AND p.owner_id=? AND p.space_id=? AND p.repo_id=?`, id, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID).Scan(&payload, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return Publication{}, authority.ErrDenied
	}
	if err != nil {
		return Publication{}, ErrUnavailable
	}
	var p Publication
	if json.Unmarshal([]byte(payload), &p) != nil {
		return Publication{}, ErrUnavailable
	}
	if receipt != "" {
		p.State = "published"
	}
	return p, nil
}

func (s *Session) ProposePublication(in PublicationInput) (Publication, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Publication{}, err
	}
	if err := validatePublication(in); err != nil {
		return Publication{}, err
	}
	if in.Destination.SpaceID == ctx.SpaceID {
		return Publication{}, authority.ErrDenied
	}
	tx, err := s.store.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Publication{}, ErrUnavailable
	}
	defer tx.Rollback()
	hash := fingerprint(in)
	var priorHash, payload string
	err = tx.QueryRow("SELECT fingerprint,payload FROM publication_revisions WHERE owner_id=? AND space_id=? AND repo_id=? AND operation_id=?", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, in.OperationID).Scan(&priorHash, &payload)
	if err == nil {
		if priorHash != hash {
			return Publication{}, ErrOperation
		}
		var p Publication
		if json.Unmarshal([]byte(payload), &p) != nil {
			return Publication{}, ErrUnavailable
		}
		return p, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Publication{}, ErrUnavailable
	}
	if _, _, err := sourceSnapshot(tx, ctx, in.Sources); err != nil {
		return Publication{}, err
	}
	id := in.ID
	if id == "" {
		id = identifier()
		if _, err := tx.Exec("INSERT INTO publication_proposals VALUES(?,?,?,?,?,?,?)", id, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, "", "", ""); err != nil {
			return Publication{}, ErrUnavailable
		}
	} else {
		p, err := lookupPublication(tx, ctx, id)
		if err != nil {
			return Publication{}, err
		}
		if p.State == "published" || p.RevisionID != in.ExpectedRevision {
			return Publication{}, ErrConflict
		}
	}
	p := Publication{ID: id, RevisionID: identifier(), State: "approval-required", Content: in.Content, Sources: in.Sources, Destination: in.Destination}
	data, _ := json.Marshal(p)
	if _, err := tx.Exec("INSERT INTO publication_revisions VALUES(?,?,?,?,?,?,?,?)", p.RevisionID, p.ID, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, in.OperationID, hash, string(data)); err != nil {
		return Publication{}, ErrUnavailable
	}
	if _, err := tx.Exec("UPDATE publication_proposals SET head=? WHERE id=?", p.RevisionID, p.ID); err != nil {
		return Publication{}, ErrUnavailable
	}
	if tx.Commit() != nil {
		return Publication{}, ErrUnavailable
	}
	return p, nil
}

func (s *Session) Publication(id string) (Publication, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Publication{}, err
	}
	if !validText(id) {
		return Publication{}, ErrInvalidText
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Publication{}, ErrUnavailable
	}
	defer tx.Rollback()
	return lookupPublication(tx, ctx, id)
}

func makePublicationReview(tx *sql.Tx, source, destination authority.Context, id string) (PublicationReview, error) {
	if source.OwnerID != destination.OwnerID || source.SpaceID == destination.SpaceID || destination.SpaceName != "Personal" {
		return PublicationReview{}, authority.ErrDenied
	}
	p, err := lookupPublication(tx, source, id)
	if err != nil {
		return PublicationReview{}, err
	}
	if p.Destination.SpaceID != destination.SpaceID || p.Destination.RepositoryID != destination.RepositoryID {
		return PublicationReview{}, authority.ErrDenied
	}
	sources, policies, err := sourceSnapshot(tx, source, p.Sources)
	if err != nil {
		return PublicationReview{}, err
	}
	policy, err := publicationPolicy(tx, destination, "space", destination.SpaceID, "publish")
	if err != nil {
		return PublicationReview{}, err
	}
	if !policy.Allowed {
		return PublicationReview{}, authority.ErrDenied
	}
	policies = append(policies, policy)
	review := PublicationReview{Proposal: p, SourceContext: source, DestinationContext: destination, Sources: sources, Policies: policies, Audience: "local owner", Placement: "private local state", Provenance: "Human-reviewed generalized lesson", Dependencies: []SourceFile{}}
	review.Proposal.State = "approval-required"
	review.Snapshot = fingerprint(review)
	return review, nil
}
