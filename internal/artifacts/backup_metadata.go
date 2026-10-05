package artifacts

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"

	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/strictjson"
)

func validateBackupMetadata(db *sql.DB, scope authority.BackupScope, repos map[string]string, version int) error {
	rows, err := db.Query("SELECT owner_id,space_id,repo_id,operation_id,fingerprint,receipt FROM contributions")
	if err != nil {
		return err
	}
	type savedReceipt struct {
		Receipt
		space, repo string
	}
	receipts := []savedReceipt{}
	for rows.Next() {
		var owner, space, repo, operation, hash, encoded string
		if err := rows.Scan(&owner, &space, &repo, &operation, &hash, &encoded); err != nil {
			rows.Close()
			return err
		}
		var receipt Receipt
		if owner != scope.OwnerID || space == "" || repos[repo] != space || !operationID(operation) || !backupHex(hash, 32) || strictjson.Decode([]byte(encoded), &receipt) != nil || receipt.OperationID != operation || !backupHex(receipt.ArtifactID, 32) || receipt.RevisionID != "" && !backupHex(receipt.RevisionID, 32) || !strings.Contains("|pending-local|candidate|conflict|conflict-retired|approved|retired|restored-awaiting-candidate|published|", "|"+receipt.State+"|") {
			rows.Close()
			return errors.New("invalid contribution receipt or scope")
		}
		receipts = append(receipts, savedReceipt{receipt, space, repo})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, receipt := range receipts {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM artifacts a WHERE a.id=? AND a.owner_id=? AND a.space_id=? AND a.repo_id=? AND (?='' OR EXISTS(SELECT 1 FROM revisions r WHERE r.id=? AND r.artifact_id=a.id))", receipt.ArtifactID, scope.OwnerID, receipt.space, receipt.repo, receipt.RevisionID, receipt.RevisionID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("receipt points outside its persisted scope")
		}
	}
	if version >= 2 {
		rows, err = db.Query("SELECT r.qualified,r.alias,a.space_id,a.repo_id,a.kind FROM registrations r JOIN artifacts a ON a.id=r.artifact_id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var qualified, alias, space, repo, kind string
			if err := rows.Scan(&qualified, &alias, &space, &repo, &kind); err != nil {
				rows.Close()
				return err
			}
			parts := strings.Split(qualified, "/")
			if kind == "memory" || len(parts) != 5 || parts[0] != space || parts[1] != repo || parts[2] != kind || !sourceName(parts[3]) || !sourceName(parts[4]) || alias != "" && !sourceName(alias) {
				rows.Close()
				return errors.New("invalid registered source identity")
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	if version >= 3 {
		rows, err = db.Query("SELECT metadata FROM revision_associations")
		if err != nil {
			return err
		}
		for rows.Next() {
			var data string
			var a Associations
			if err := rows.Scan(&data); err != nil {
				rows.Close()
				return err
			}
			if strictjson.Decode([]byte(data), &a) != nil || a.validate() != nil {
				rows.Close()
				return errors.New("invalid revision associations")
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		var broken int
		if err := db.QueryRow(`SELECT count(*) FROM revision_associations m
 JOIN revisions r ON r.id=m.revision_id JOIN artifacts a ON a.id=r.artifact_id
 JOIN json_each(m.metadata,'$.related') edge
 WHERE edge.type='text' AND NOT EXISTS(SELECT 1 FROM artifacts target WHERE target.id=edge.value
 AND target.owner_id=a.owner_id AND target.space_id=a.space_id AND target.repo_id=a.repo_id)`).Scan(&broken); err != nil {
			return err
		}
		if broken != 0 {
			return errors.New("related artifact points outside its persisted scope")
		}
	}
	if version >= 4 {
		return validateBackupPublications(db, scope, repos)
	}
	return nil
}

func validateBackupPublications(db *sql.DB, scope authority.BackupScope, repos map[string]string) error {
	spaces := map[string]bool{}
	for _, id := range scope.SpaceIDs {
		spaces[id] = true
	}
	checks := []string{
		"SELECT count(*) FROM publication_proposals p WHERE NOT EXISTS(SELECT 1 FROM publication_revisions r WHERE r.id=p.head AND r.proposal_id=p.id AND r.owner_id=p.owner_id AND r.space_id=p.space_id AND r.repo_id=p.repo_id)",
		"SELECT count(*) FROM publication_revisions r JOIN publication_proposals p ON p.id=r.proposal_id WHERE r.owner_id<>p.owner_id OR r.space_id<>p.space_id OR r.repo_id<>p.repo_id",
		"SELECT count(*) FROM publication_proposals WHERE (receipt='')<>(review='')",
	}
	for _, query := range checks {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("invalid publication history or private audit")
		}
	}
	rows, err := db.Query("SELECT owner_id,scope,scope_id,action,epoch FROM publication_policy")
	if err != nil {
		return err
	}
	type policyRef struct{ scope, id string }
	policies := []policyRef{}
	for rows.Next() {
		var owner, category, id, action string
		var epoch int64
		if err := rows.Scan(&owner, &category, &id, &action, &epoch); err != nil {
			rows.Close()
			return err
		}
		if owner != scope.OwnerID || epoch < 1 || category != "space" && category != "artifact" || action != "export" && action != "publish" || category == "artifact" && action != "export" || category == "space" && !spaces[id] {
			rows.Close()
			return errors.New("invalid publication policy")
		}
		policies = append(policies, policyRef{category, id})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, p := range policies {
		if p.scope == "artifact" {
			var count int
			if err := db.QueryRow("SELECT count(*) FROM artifacts WHERE id=? AND owner_id=?", p.id, scope.OwnerID).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return errors.New("publication policy refers to missing artifact")
			}
		}
	}
	rows, err = db.Query("SELECT id,proposal_id,owner_id,space_id,repo_id,operation_id,fingerprint,payload FROM publication_revisions")
	if err != nil {
		return err
	}
	type proposalRef struct {
		publication Publication
		space, repo string
	}
	proposals := []proposalRef{}
	for rows.Next() {
		var id, proposal, owner, space, repo, operation, hash, encoded string
		if err := rows.Scan(&id, &proposal, &owner, &space, &repo, &operation, &hash, &encoded); err != nil {
			rows.Close()
			return err
		}
		var p Publication
		if !backupHex(id, 32) || !backupHex(proposal, 32) || owner != scope.OwnerID || space == "" || repos[repo] != space || !operationID(operation) || !backupHex(hash, 32) || strictjson.Decode([]byte(encoded), &p) != nil || p.ID != proposal || p.RevisionID != id || p.State != "approval-required" || p.Destination.SpaceID == space || len(p.Destination.SpaceID) != 64 || len(p.Destination.RepositoryID) != 64 || !validText(p.Destination.SpaceID, p.Destination.RepositoryID) || len(p.Sources) < 1 || len(p.Sources) > 32 || len(p.Content) > 1024*1024 || strings.TrimSpace(p.Content) == "" || !validText(p.Content) {
			rows.Close()
			return errors.New("invalid publication revision")
		}
		proposals = append(proposals, proposalRef{p, space, repo})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, p := range proposals {
		seen := map[string]bool{}
		for _, src := range p.publication.Sources {
			var count int
			if !backupHex(src.ArtifactID, 32) || !backupHex(src.RevisionID, 32) || seen[src.ArtifactID] {
				return errors.New("invalid publication source references")
			}
			seen[src.ArtifactID] = true
			if err := db.QueryRow("SELECT count(*) FROM artifacts a JOIN revisions r ON r.artifact_id=a.id WHERE a.id=? AND r.id=? AND a.owner_id=? AND a.space_id=? AND a.repo_id=?", src.ArtifactID, src.RevisionID, scope.OwnerID, p.space, p.repo).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return errors.New("publication source points outside its persisted scope")
			}
		}
	}
	rows, err = db.Query("SELECT id,head,receipt,review FROM publication_proposals WHERE receipt<>''")
	if err != nil {
		return err
	}
	type completedPublication struct {
		Receipt
		review PublicationReview
	}
	completed := []completedPublication{}
	for rows.Next() {
		var id, head, encoded, audit string
		var receipt Receipt
		var review PublicationReview
		if err := rows.Scan(&id, &head, &encoded, &audit); err != nil {
			rows.Close()
			return err
		}
		if strictjson.Decode([]byte(encoded), &receipt) != nil || strictjson.Decode([]byte(audit), &review) != nil || receipt.State != "published" || review.Proposal.ID != id || review.Proposal.RevisionID != head || review.SourceContext.OwnerID != scope.OwnerID || review.DestinationContext.OwnerID != scope.OwnerID || !backupHex(review.Snapshot, 32) {
			rows.Close()
			return errors.New("invalid completed private publication audit")
		}
		completed = append(completed, completedPublication{receipt, review})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, p := range completed {
		review := p.review
		hash := review.Snapshot
		review.Snapshot = ""
		if fingerprint(review) != hash || !backupContext(scope, review.SourceContext) || !backupContext(scope, review.DestinationContext) || review.DestinationContext.SpaceName != "Personal" || review.SourceContext.SpaceID == review.DestinationContext.SpaceID || !operationID(p.OperationID) || !backupHex(p.ArtifactID, 32) || !backupHex(p.RevisionID, 32) {
			return errors.New("invalid completed publication snapshot or recipient")
		}
		var encoded string
		if err := db.QueryRow("SELECT payload FROM publication_revisions WHERE id=? AND owner_id=? AND space_id=? AND repo_id=?", review.Proposal.RevisionID, scope.OwnerID, review.SourceContext.SpaceID, review.SourceContext.RepositoryID).Scan(&encoded); err != nil {
			return err
		}
		var proposal Publication
		if strictjson.Decode([]byte(encoded), &proposal) != nil || !reflect.DeepEqual(proposal, review.Proposal) {
			return errors.New("private audit does not match the published proposal")
		}
		var count int
		if err := db.QueryRow("SELECT count(*) FROM artifacts a JOIN revisions r ON r.artifact_id=a.id WHERE a.id=? AND a.owner_id=? AND a.space_id=? AND a.repo_id=? AND a.kind='memory' AND r.id=? AND r.content=? AND r.provenance=?", p.ArtifactID, scope.OwnerID, review.DestinationContext.SpaceID, review.DestinationContext.RepositoryID, p.RevisionID, review.Proposal.Content, review.Provenance).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("private publication receipt does not match its recipient revision")
		}
	}
	return nil
}

func backupContext(scope authority.BackupScope, ctx authority.Context) bool {
	for _, known := range scope.Contexts {
		if known == ctx {
			return true
		}
	}
	return false
}

// PrepareRestored migrates only an unpublished restore stage and discards review credentials.
func PrepareRestored(auth *authority.Store) error {
	store, err := Open(auth)
	if err != nil {
		return err
	}
	_, err = store.db.Exec("DELETE FROM review_grants")
	return errors.Join(err, store.Close())
}
