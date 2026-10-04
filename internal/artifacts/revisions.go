package artifacts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func identifier() string {
	value := make([]byte, 32)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}
func fingerprint(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func operationID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\x00\r\n\t")
}

func validText(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func (s *Session) Contribute(c Contribution) (Receipt, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Receipt{}, err
	}
	if !validText(c.OperationID, c.ArtifactID, c.ExpectedRevision, c.SpaceID, c.RepositoryID, c.Kind, c.Content, c.Provenance) || (c.Source != nil && !validText(c.Source.Commit, c.Source.Path, c.Source.Blob)) {
		return Receipt{}, ErrInvalidText
	}
	if c.SpaceID == "" {
		c.SpaceID = ctx.SpaceID
	}
	if c.RepositoryID == "" {
		c.RepositoryID = ctx.RepositoryID
	}
	if err := ctx.Authorize(c.SpaceID, c.RepositoryID); err != nil {
		return Receipt{}, err
	}
	if !operationID(c.OperationID) || len(c.Content) > 1024*1024 || len(c.Provenance) > 4096 || (c.Kind != "memory" && c.Kind != "skill" && c.Kind != "agent-definition") || (c.ArtifactID == "" && c.ExpectedRevision != "") {
		return Receipt{}, errors.New("invalid contribution: supply an operation ID, supported kind, and bounded content/provenance")
	}
	if c.Kind == "memory" && (strings.TrimSpace(c.Content) == "" || c.Source != nil) {
		return Receipt{}, errors.New("memory requires observation content and cannot replace Git source")
	}
	if c.Kind != "memory" && (c.Source == nil || c.Content != "") {
		return Receipt{}, errors.New("skills and definitions require immutable Git source; content is read from Git")
	}
	if c.Source != nil {
		for _, file := range c.Source.Files {
			if !validText(file.Path, file.Blob, file.Content) {
				return Receipt{}, ErrInvalidText
			}
		}
	}
	hash := fingerprint(c)
	tx, err := s.store.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	defer tx.Rollback()
	if receipt, found, err := retry(tx, ctx, c.OperationID, hash); err != nil || found {
		return receipt, err
	}
	a := Artifact{ID: c.ArtifactID, SpaceID: ctx.SpaceID, RepositoryID: ctx.RepositoryID, Kind: c.Kind, Lifecycle: "active"}
	if a.ID == "" {
		a.ID = identifier()
		if _, err := tx.Exec("INSERT INTO artifacts VALUES(?,?,?,?,?,?,?)", a.ID, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, a.Kind, a.Lifecycle, ""); err != nil {
			return Receipt{}, ErrUnavailable
		}
	} else {
		var err error
		a, err = lookup(tx, ctx, a.ID)
		if err != nil {
			return Receipt{}, err
		}
		if a.Kind != c.Kind {
			return Receipt{}, errors.New("artifact kind cannot change")
		}
	}
	var source string
	state := "pending-local"
	if c.Kind != "memory" {
		content, src, err := readBundle(ctx.Checkout, *c.Source)
		if err != nil {
			return Receipt{}, err
		}
		c.Content = content
		encoded, _ := json.Marshal(src)
		source = string(encoded)
		state = "candidate"
	}
	if a.Lifecycle == "retired" {
		state = "conflict-retired"
	} else if c.ExpectedRevision != a.HeadRevision {
		state = "conflict"
	}
	r := Receipt{OperationID: c.OperationID, ArtifactID: a.ID, RevisionID: identifier(), State: state}
	if _, err := tx.Exec("INSERT INTO revisions VALUES(?,?,?,?,?,?,?,?,?)", r.RevisionID, a.ID, c.ExpectedRevision, c.Content, c.Provenance, ctx.OwnerID, state, "unverified", source); err != nil {
		return Receipt{}, ErrUnavailable
	}
	if state == "pending-local" {
		if _, err := tx.Exec("UPDATE artifacts SET head=? WHERE id=?", r.RevisionID, a.ID); err != nil {
			return Receipt{}, ErrUnavailable
		}
	}
	if err := record(tx, ctx, hash, r, "contribute"); err != nil {
		return Receipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Receipt{}, ErrUnavailable
	}
	return r, nil
}

func retry(tx *sql.Tx, ctx authority.Context, operation, hash string) (Receipt, bool, error) {
	var savedHash, encoded string
	err := tx.QueryRow("SELECT fingerprint, receipt FROM contributions WHERE owner_id=? AND space_id=? AND repo_id=? AND operation_id=?", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, operation).Scan(&savedHash, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, ErrUnavailable
	}
	if hash != savedHash {
		return Receipt{}, true, ErrOperation
	}
	var r Receipt
	if err := json.Unmarshal([]byte(encoded), &r); err != nil {
		return Receipt{}, true, ErrUnavailable
	}
	return r, true, nil
}

func record(tx *sql.Tx, ctx authority.Context, hash string, r Receipt, action string) error {
	encoded, _ := json.Marshal(r)
	if _, err := tx.Exec("INSERT INTO contributions VALUES(?,?,?,?,?,?)", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, r.OperationID, hash, string(encoded)); err != nil {
		return ErrUnavailable
	}
	if _, err := tx.Exec("INSERT INTO pending VALUES(?,?,?,?,?,?,?)", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, r.OperationID, r.ArtifactID, r.RevisionID, action); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Session) Retire(id, expected, operation string) (Receipt, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Receipt{}, err
	}
	if !validText(id, expected, operation) {
		return Receipt{}, ErrInvalidText
	}
	if !operationID(operation) {
		return Receipt{}, errors.New("retirement requires an operation ID")
	}
	hash := fingerprint(struct{ Action, ID, Expected string }{"retire", id, expected})
	tx, err := s.store.db.BeginTx(context.Background(), nil)
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
	if a.HeadRevision != expected {
		return Receipt{}, ErrConflict
	}
	if _, err := tx.Exec("UPDATE artifacts SET lifecycle='retired' WHERE id=?", id); err != nil {
		return Receipt{}, ErrUnavailable
	}
	r := Receipt{OperationID: operation, ArtifactID: id, RevisionID: a.HeadRevision, State: "retired"}
	if err := record(tx, ctx, hash, r, "retire"); err != nil {
		return Receipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Receipt{}, ErrUnavailable
	}
	return r, nil
}
