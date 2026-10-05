package artifacts

import (
	"context"
	"database/sql"

	"github.com/agentic-substrate/substrate/internal/authority"
)

const maintenanceSchema = `
CREATE TABLE maintenance(singleton INTEGER PRIMARY KEY CHECK(singleton=1), paused INTEGER NOT NULL CHECK(paused IN (0,1)));
INSERT INTO maintenance VALUES(1,0);
ALTER TABLE index_queue ADD COLUMN bulk INTEGER NOT NULL DEFAULT 0 CHECK(bulk IN (0,1));
ALTER TABLE index_queue ADD COLUMN failure TEXT NOT NULL DEFAULT '';
PRAGMA user_version=5;
`

type Maintenance struct {
	Mode     string      `json:"mode"`
	Paused   bool        `json:"paused"`
	State    string      `json:"state"`
	Queued   int         `json:"queued"`
	Deferred int         `json:"deferred"`
	Failed   int         `json:"failed"`
	Coverage *IndexState `json:"coverage,omitempty"`
}

func (s *Store) SetIndexPaused(paused bool) error {
	if _, err := s.db.Exec("UPDATE maintenance SET paused=? WHERE singleton=1", paused); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) IndexPaused() (bool, error) {
	var paused bool
	if err := s.db.QueryRow("SELECT paused FROM maintenance WHERE singleton=1").Scan(&paused); err != nil {
		return false, ErrUnavailable
	}
	return paused, nil
}

func (s *Store) RebuildIndex() error {
	_, err := s.db.Exec("INSERT INTO index_queue(artifact_id,bulk) SELECT id,1 FROM artifacts WHERE true ON CONFLICT(artifact_id) DO UPDATE SET bulk=1")
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) RetryIndex() error {
	if _, err := s.db.Exec("UPDATE index_queue SET failure='' WHERE failure<>''"); err != nil {
		return ErrUnavailable
	}
	return nil
}

func maintenance(tx *sql.Tx, scope *authority.Context) (Maintenance, error) {
	m := Maintenance{Mode: "lexical", State: "ready"}
	if err := tx.QueryRow("SELECT paused FROM maintenance WHERE singleton=1").Scan(&m.Paused); err != nil {
		return Maintenance{}, ErrUnavailable
	}
	where := ""
	args := []any{}
	if scope != nil {
		where = " WHERE a.owner_id=? AND a.space_id=? AND a.repo_id=?"
		args = []any{scope.OwnerID, scope.SpaceID, scope.RepositoryID}
	}
	rows, err := tx.Query("SELECT q.bulk,q.failure FROM index_queue q JOIN artifacts a ON a.id=q.artifact_id"+where, args...)
	if err != nil {
		return Maintenance{}, ErrUnavailable
	}
	for rows.Next() {
		var bulk bool
		var failure string
		if rows.Scan(&bulk, &failure) != nil {
			rows.Close()
			return Maintenance{}, ErrUnavailable
		}
		switch {
		case failure != "":
			m.Failed++
		case bulk:
			m.Deferred++
		default:
			m.Queued++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Maintenance{}, ErrUnavailable
	}
	switch {
	case m.Failed > 0:
		m.State = "failed"
	case m.Queued > 0:
		m.State = "pending"
	case m.Deferred > 0:
		m.State = "deferred"
	}
	if scope != nil {
		all, err := eligible(tx, *scope)
		if err != nil {
			return Maintenance{}, err
		}
		state, err := coverage(tx, all)
		if err != nil {
			return Maintenance{}, err
		}
		m.Coverage = &state
	}
	return m, nil
}

func (s *Store) Maintenance() (Maintenance, error) {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Maintenance{}, ErrUnavailable
	}
	defer tx.Rollback()
	return maintenance(tx, nil)
}

func coverage(tx *sql.Tx, all []current) (IndexState, error) {
	state := IndexState{Eligible: len(all)}
	for _, c := range all {
		var revision string
		var limited bool
		err := tx.QueryRow("SELECT revision_id,limited FROM indexed WHERE artifact_id=?", c.id).Scan(&revision, &limited)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return IndexState{}, ErrUnavailable
		}
		if revision == c.revision {
			state.Indexed++
			if limited {
				state.Limited++
			}
		}
	}
	state.Pending = state.Eligible - state.Indexed
	return state, nil
}
