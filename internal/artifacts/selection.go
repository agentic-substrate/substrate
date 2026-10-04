package artifacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type Owner struct {
	store    *Store
	checkout string
}
type Registration struct {
	ArtifactID  string `json:"artifact_id"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	Overridable bool   `json:"overridable"`
}
type Approval struct {
	OperationID      string `json:"operation_id"`
	ArtifactID       string `json:"artifact_id"`
	RevisionID       string `json:"revision_id"`
	ExpectedRevision string `json:"expected_revision"`
	Overrides        string `json:"overrides,omitempty"`
	OverrideRevision string `json:"override_revision,omitempty"`
}
type Choice struct {
	Overridable      bool   `json:"overridable"`
	Overrides        string `json:"overrides,omitempty"`
	OverrideRevision string `json:"override_revision,omitempty"`
	ArtifactID       string `json:"artifact_id"`
	Qualified        string `json:"qualified"`
	Alias            string `json:"alias"`
	RevisionID       string `json:"revision_id"`
	State            string `json:"state"`
	Reason           string `json:"reason"`
}
type Delivery struct {
	Choice           Choice   `json:"choice"`
	Revision         Revision `json:"revision"`
	NativeActivation string   `json:"native_activation"`
}

func (s *Store) Owner(checkout string) (*Owner, error) {
	if _, err := s.authority.OwnerContext(checkout); err != nil {
		return nil, err
	}
	return &Owner{s, checkout}, nil
}

type selection struct {
	Choice
	lifecycle, kind  string
	relationConflict bool
}

func selections(tx *sql.Tx, ctx authority.Context) ([]selection, error) {
	rows, err := tx.Query(`SELECT a.id, g.qualified, g.alias, a.head, a.lifecycle, a.kind,
 g.overridable, coalesce(p.overrides,''), coalesce(p.override_revision,'')
 FROM artifacts a JOIN registrations g ON g.artifact_id=a.id
 LEFT JOIN approvals p ON p.revision_id=a.head
 WHERE a.owner_id=? AND a.space_id=? AND a.repo_id=? ORDER BY g.qualified`, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	result := []selection{}
	for rows.Next() {
		var c selection
		if err := rows.Scan(&c.ArtifactID, &c.Qualified, &c.Alias, &c.RevisionID, &c.lifecycle, &c.kind, &c.Overridable, &c.Overrides, &c.OverrideRevision); err != nil {
			return nil, ErrUnavailable
		}
		c.State, c.Reason = "effective", "approved revision selected"
		if c.RevisionID == "" {
			c.State, c.Reason = "candidate", "explicit owner approval required"
		}
		if c.lifecycle == "retired" {
			c.State, c.Reason = "retired", "retirement blocks delivery"
		}
		result = append(result, c)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	byID := make(map[string]int, len(result))
	for i, c := range result {
		byID[c.ArtifactID] = i
	}
	for i, c := range result {
		if c.State != "effective" || c.Overrides == "" {
			continue
		}
		j, ok := byID[c.Overrides]
		if !ok || result[j].lifecycle != "active" || result[j].RevisionID != c.OverrideRevision || !result[j].Overridable || result[j].Overrides != "" {
			result[i].State, result[i].Reason = "conflict", "approved override target changed or retired"
			result[i].relationConflict = true
		} else {
			result[j].State, result[j].Reason = "overridden", "explicit approved specialization applies to alias"
		}
	}
	aliases := map[string][]int{}
	for i, c := range result {
		if c.Alias != "" && (c.State == "effective" || c.State == "conflict") {
			aliases[c.Alias] = append(aliases[c.Alias], i)
		}
	}
	for _, indices := range aliases {
		blocked := len(indices) > 1
		for _, i := range indices {
			blocked = blocked || result[i].relationConflict
		}
		if blocked {
			for _, i := range indices {
				if !result[i].relationConflict {
					result[i].State, result[i].Reason = "conflict", "alias has incomparable approved choices"
				}
			}
		}
	}
	return result, nil
}

func (s *Session) Choices(selector string) ([]Choice, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()
	all, err := selections(tx, ctx)
	if err != nil {
		return nil, err
	}
	result := []Choice{}
	for _, c := range all {
		if selector == "" || selector == c.Qualified || selector == c.Alias {
			result = append(result, c.Choice)
		}
	}
	return result, nil
}

func (s *Session) Resolve(selector string) (Delivery, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Delivery{}, err
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Delivery{}, ErrUnavailable
	}
	defer tx.Rollback()
	all, err := selections(tx, ctx)
	if err != nil {
		return Delivery{}, err
	}
	selected := -1
	for i, c := range all {
		if selector == c.Qualified {
			if c.relationConflict {
				return Delivery{}, ErrConflict
			}
			if c.lifecycle != "active" || c.RevisionID == "" {
				return Delivery{}, authority.ErrDenied
			}
			selected = i
			break
		}
		if selector != "" && selector == c.Alias {
			if c.State == "conflict" {
				return Delivery{}, ErrConflict
			}
			if c.State == "effective" {
				if selected != -1 {
					return Delivery{}, ErrConflict
				}
				selected = i
			}
		}
	}
	if selected == -1 {
		return Delivery{}, authority.ErrDenied
	}
	c := all[selected].Choice
	c.State, c.Reason = "effective", "explicit qualified identity or unambiguous approved alias"
	r, err := loadRevision(tx, c.ArtifactID, c.RevisionID)
	if err != nil {
		return Delivery{}, err
	}
	if r.State != "approved" || r.Source == nil {
		return Delivery{}, ErrUnavailable
	}
	return Delivery{Choice: c, Revision: r, NativeActivation: "unsupported"}, nil
}

func loadRevision(tx *sql.Tx, artifact, id string) (Revision, error) {
	var r Revision
	var source string
	err := tx.QueryRow("SELECT id,base,content,provenance,author_id,state,verification,source FROM revisions WHERE artifact_id=? AND id=?", artifact, id).Scan(&r.ID, &r.Base, &r.Content, &r.Provenance, &r.AuthorID, &r.State, &r.Verification, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, authority.ErrDenied
	}
	if err != nil {
		return Revision{}, ErrUnavailable
	}
	if source != "" {
		if err := json.Unmarshal([]byte(source), &r.Source); err != nil {
			return Revision{}, ErrUnavailable
		}
	}
	return r, nil
}
