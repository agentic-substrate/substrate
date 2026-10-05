package artifacts

import (
	"context"
	"database/sql"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type ArtifactSummary struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Lifecycle    string `json:"lifecycle"`
	HeadRevision string `json:"head_revision"`
}
type PublicationSummary struct {
	ID          string                 `json:"id"`
	RevisionID  string                 `json:"revision_id"`
	State       string                 `json:"state"`
	Destination PublicationDestination `json:"destination"`
}
type BrowserInventory struct {
	Context   authority.Context    `json:"context"`
	Artifacts []ArtifactSummary    `json:"artifacts"`
	Proposals []PublicationSummary `json:"proposals"`
}
type PermittedAction struct {
	Action string `json:"action"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}
type BrowserInspection struct {
	Artifact Artifact          `json:"artifact"`
	Choices  []Choice          `json:"choices"`
	Actions  []PermittedAction `json:"actions"`
}

func (s *Session) BrowserInventory() (BrowserInventory, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return BrowserInventory{}, err
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BrowserInventory{}, ErrUnavailable
	}
	defer tx.Rollback()
	result := BrowserInventory{Context: ctx, Artifacts: []ArtifactSummary{}, Proposals: []PublicationSummary{}}
	rows, err := tx.Query("SELECT id,kind,lifecycle,head FROM artifacts WHERE owner_id=? AND space_id=? AND repo_id=? ORDER BY id LIMIT 100", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID)
	if err != nil {
		return BrowserInventory{}, ErrUnavailable
	}
	for rows.Next() {
		var a ArtifactSummary
		if rows.Scan(&a.ID, &a.Kind, &a.Lifecycle, &a.HeadRevision) != nil {
			rows.Close()
			return BrowserInventory{}, ErrUnavailable
		}
		result.Artifacts = append(result.Artifacts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return BrowserInventory{}, ErrUnavailable
	}
	rows, err = tx.Query("SELECT id FROM publication_proposals WHERE owner_id=? AND space_id=? AND repo_id=? ORDER BY id LIMIT 100", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID)
	if err != nil {
		return BrowserInventory{}, ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return BrowserInventory{}, ErrUnavailable
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return BrowserInventory{}, ErrUnavailable
	}
	for _, id := range ids {
		p, err := lookupPublication(tx, ctx, id)
		if err != nil {
			return BrowserInventory{}, err
		}
		result.Proposals = append(result.Proposals, PublicationSummary{ID: p.ID, RevisionID: p.RevisionID, State: p.State, Destination: p.Destination})
	}
	return result, nil
}

func (s *Session) InspectBrowser(id string) (BrowserInspection, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return BrowserInspection{}, err
	}
	if !validText(id) {
		return BrowserInspection{}, ErrInvalidText
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BrowserInspection{}, ErrUnavailable
	}
	defer tx.Rollback()
	a, err := inspect(tx, ctx, id)
	if err != nil {
		return BrowserInspection{}, err
	}
	all, err := selections(tx, ctx)
	if err != nil {
		return BrowserInspection{}, err
	}
	result := BrowserInspection{Artifact: a, Choices: []Choice{}, Actions: []PermittedAction{{Action: "inspect", State: "allowed", Reason: "Current session permits this repository's artifact history."}, {Action: "contribute", State: "allowed", Reason: "Contributions stay in this repository and space; stale or retired edits remain restricted candidates."}, {Action: "native-activation", State: "unavailable", Reason: "Native installation and execution are not implemented."}}}
	for _, c := range all {
		if c.ArtifactID == id {
			result.Choices = append(result.Choices, c.Choice)
		}
	}
	read := PermittedAction{Action: "read", State: "denied", Reason: "Only current active memories and approved Git snapshots are delivered."}
	eligibleSources, err := eligible(tx, ctx)
	if err != nil {
		return BrowserInspection{}, err
	}
	for _, c := range eligibleSources {
		if c.id == id {
			read.State = "allowed"
			read.Reason = "Exact current revision is permitted; content reads do not execute source."
		}
	}
	publication := PermittedAction{Action: "publication", State: "denied", Reason: "Source export is denied or the source is not current and eligible."}
	if _, _, err := sourceSnapshot(tx, ctx, []PublicationSource{{ArtifactID: id, RevisionID: a.HeadRevision}}); err == nil {
		publication.State = "approval-required"
		publication.Reason = "A derived lesson needs independently authorized Personal destination and exact human review."
	} else if err == ErrUnavailable {
		return BrowserInspection{}, err
	}
	result.Actions = append(result.Actions, read, publication)
	return result, nil
}
