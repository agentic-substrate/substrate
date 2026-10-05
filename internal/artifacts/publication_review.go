package artifacts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func reviewDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (o *Owner) IssuePublicationReview(id, revision, destination string) (string, error) {
	if !validText(id, revision, destination) {
		return "", ErrInvalidText
	}
	var token string
	err := o.store.authority.WithOwnerContexts([]string{o.checkout, destination}, func(contexts []authority.Context) error {
		tx, err := o.store.db.BeginTx(context.Background(), nil)
		if err != nil {
			return ErrUnavailable
		}
		defer tx.Rollback()
		review, err := makePublicationReview(tx, contexts[0], contexts[1], id)
		if err != nil {
			return err
		}
		if review.Proposal.RevisionID != revision {
			return ErrConflict
		}
		p, err := lookupPublication(tx, contexts[0], id)
		if err != nil {
			return err
		}
		if p.State == "published" {
			return ErrConflict
		}
		token = identifier()
		data, _ := json.Marshal(review)
		if len(data) > 7*1024*1024 {
			return fmt.Errorf("%w: review snapshot exceeds seven MiB; reduce its source inventory or content", ErrUnavailable)
		}
		if _, err := tx.Exec("INSERT INTO review_grants VALUES(?,?,?,?,?)", reviewDigest(token), string(data), time.Now().Add(15*time.Minute).UnixNano(), "", ""); err != nil {
			return ErrUnavailable
		}
		if tx.Commit() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (o *Owner) RevokePublicationReview(token string) error {
	return o.store.authority.WithOwnerContexts([]string{o.checkout}, func(contexts []authority.Context) error {
		var snapshot string
		err := o.store.db.QueryRow("SELECT snapshot FROM review_grants WHERE token_hash=?", reviewDigest(token)).Scan(&snapshot)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		var review PublicationReview
		if json.Unmarshal([]byte(snapshot), &review) != nil {
			return ErrUnavailable
		}
		ctx := contexts[0]
		if review.SourceContext.OwnerID != ctx.OwnerID || review.SourceContext.SpaceID != ctx.SpaceID || review.SourceContext.RepositoryID != ctx.RepositoryID {
			return authority.ErrDenied
		}
		if _, err := o.store.db.Exec("DELETE FROM review_grants WHERE token_hash=?", reviewDigest(token)); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func (s *Store) withPublicationReview(token string, historical bool, action func(*sql.Tx, PublicationReview, string, string) error) error {
	if len(token) != 64 {
		return authority.ErrDenied
	}
	var snapshot string
	var expiry int64
	err := s.db.QueryRow("SELECT snapshot,expires_at FROM review_grants WHERE token_hash=?", reviewDigest(token)).Scan(&snapshot, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return authority.ErrDenied
	}
	if err != nil {
		return ErrUnavailable
	}
	if time.Now().UnixNano() >= expiry {
		return authority.ErrDenied
	}
	var saved PublicationReview
	if json.Unmarshal([]byte(snapshot), &saved) != nil {
		return ErrUnavailable
	}
	return s.authority.WithOwnerContexts([]string{saved.SourceContext.Checkout, saved.DestinationContext.Checkout}, func(contexts []authority.Context) error {
		if contexts[0] != saved.SourceContext || contexts[1] != saved.DestinationContext {
			return authority.ErrDenied
		}
		tx, err := s.db.BeginTx(context.Background(), nil)
		if err != nil {
			return ErrUnavailable
		}
		defer tx.Rollback()
		var receipt, operation, storedSnapshot string
		if err := tx.QueryRow("SELECT snapshot,expires_at,receipt,operation_id FROM review_grants WHERE token_hash=?", reviewDigest(token)).Scan(&storedSnapshot, &expiry, &receipt, &operation); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return authority.ErrDenied
			}
			return ErrUnavailable
		}
		if time.Now().UnixNano() >= expiry || storedSnapshot != snapshot {
			return authority.ErrDenied
		}
		if historical && receipt != "" {
			return action(tx, saved, receipt, operation)
		}
		current, err := makePublicationReview(tx, contexts[0], contexts[1], saved.Proposal.ID)
		if err != nil {
			return err
		}
		if current.Snapshot != saved.Snapshot {
			return ErrConflict
		}
		return action(tx, current, receipt, operation)
	})
}

func (s *Store) ReviewPublication(token string) (PublicationReview, error) {
	var review PublicationReview
	err := s.withPublicationReview(token, false, func(tx *sql.Tx, current PublicationReview, receipt, operation string) error {
		review = current
		if receipt != "" {
			review.Proposal.State = "published"
		}
		return nil
	})
	return review, err
}

func (s *Store) Publish(token string, a PublicationApproval) (Receipt, error) {
	var receipt Receipt
	err := s.withPublicationReview(token, true, func(tx *sql.Tx, review PublicationReview, encoded, operation string) error {
		if !validText(a.OperationID, a.RevisionID, a.Snapshot) {
			return ErrInvalidText
		}
		if !operationID(a.OperationID) || len(a.RevisionID) > 128 || len(a.Snapshot) > 128 {
			return ErrConflict
		}
		if a.RevisionID != review.Proposal.RevisionID || a.Snapshot != review.Snapshot {
			return ErrConflict
		}
		if encoded != "" {
			if operation != a.OperationID {
				return ErrConflict
			}
			if json.Unmarshal([]byte(encoded), &receipt) != nil {
				return ErrUnavailable
			}
			return nil
		}
		p, err := lookupPublication(tx, review.SourceContext, review.Proposal.ID)
		if err != nil {
			return err
		}
		if p.State == "published" {
			return ErrConflict
		}
		ctx := review.DestinationContext
		id, revision := identifier(), identifier()
		if _, err := tx.Exec("INSERT INTO artifacts VALUES(?,?,?,?,?,?,?)", id, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, "memory", "active", revision); err != nil {
			return ErrUnavailable
		}
		if _, err := tx.Exec("INSERT INTO revisions VALUES(?,?,?,?,?,?,?,?,?)", revision, id, "", p.Content, review.Provenance, ctx.OwnerID, "pending-local", "unverified", ""); err != nil {
			return ErrUnavailable
		}
		receipt = Receipt{OperationID: a.OperationID, ArtifactID: id, RevisionID: revision, State: "published"}
		if err := saveAssociations(tx, ctx, revision, Associations{}); err != nil {
			return err
		}
		if err := record(tx, ctx, fingerprint(a), receipt, "publish", true); err != nil {
			return err
		}
		data, _ := json.Marshal(receipt)
		reviewData, _ := json.Marshal(review)
		if _, err := tx.Exec("UPDATE publication_proposals SET receipt=?,review=? WHERE id=?", string(data), string(reviewData), p.ID); err != nil {
			return ErrUnavailable
		}
		if _, err := tx.Exec("UPDATE review_grants SET receipt=?,operation_id=? WHERE token_hash=?", string(data), a.OperationID, reviewDigest(token)); err != nil {
			return ErrUnavailable
		}
		// Recheck expiry immediately before the grant and artifact become durable.
		var expires int64
		if err := tx.QueryRow("SELECT expires_at FROM review_grants WHERE token_hash=?", reviewDigest(token)).Scan(&expires); err != nil {
			return ErrUnavailable
		}
		if time.Now().UnixNano() >= expires {
			return authority.ErrDenied
		}
		if tx.Commit() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}
