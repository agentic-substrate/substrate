package artifacts

import (
	"context"
	"errors"
	"strings"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func sourceName(value string) bool {
	if len(value) == 0 || len(value) > 80 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return value != "." && value != ".."
}

func (o *Owner) Register(r Registration) (Choice, error) {
	ctx, err := o.store.authority.OwnerContext(o.checkout)
	if err != nil {
		return Choice{}, err
	}
	if !sourceName(r.Source) || !sourceName(r.Name) || r.Alias != "" && !sourceName(r.Alias) {
		return Choice{}, errors.New("source, name, and optional alias require 1–80 lowercase letters, digits, dots, underscores, or hyphens")
	}
	tx, err := o.store.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Choice{}, ErrUnavailable
	}
	defer tx.Rollback()
	a, err := lookup(tx, ctx, r.ArtifactID)
	if err != nil {
		return Choice{}, err
	}
	if a.Kind == "memory" {
		return Choice{}, authority.ErrDenied
	}
	qualified := strings.Join([]string{ctx.SpaceID, ctx.RepositoryID, a.Kind, r.Source, r.Name}, "/")
	all, err := selections(tx, ctx)
	if err != nil {
		return Choice{}, err
	}
	for _, c := range all {
		if c.ArtifactID == r.ArtifactID {
			if c.Qualified != qualified || c.Alias != r.Alias || c.overridable != r.Overridable {
				return Choice{}, ErrConflict
			}
			return c.Choice, nil
		}
		if c.Qualified == qualified {
			return Choice{}, ErrConflict
		}
	}
	if a.Lifecycle != "active" {
		return Choice{}, ErrConflict
	}
	if _, err := tx.Exec("INSERT INTO registrations VALUES(?,?,?,?)", r.ArtifactID, qualified, r.Alias, r.Overridable); err != nil {
		return Choice{}, ErrUnavailable
	}
	if err := tx.Commit(); err != nil {
		return Choice{}, ErrUnavailable
	}
	return Choice{ArtifactID: r.ArtifactID, Qualified: qualified, Alias: r.Alias, State: "candidate", Reason: "explicit owner approval required"}, nil
}
