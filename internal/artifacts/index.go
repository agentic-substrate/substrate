package artifacts

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
)

const MaxIndexedTokens = 32768

const retrievalSchema = `
CREATE TABLE revision_associations(revision_id TEXT PRIMARY KEY REFERENCES revisions(id), metadata TEXT NOT NULL);
CREATE TABLE index_queue(artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id));
CREATE TABLE indexed(artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id), revision_id TEXT NOT NULL, limited INTEGER NOT NULL);
CREATE TABLE tokens(artifact_id TEXT NOT NULL REFERENCES artifacts(id), revision_id TEXT NOT NULL, token TEXT NOT NULL, position INTEGER NOT NULL);
CREATE INDEX token_lookup ON tokens(token,artifact_id,revision_id);
INSERT INTO index_queue SELECT id FROM artifacts;
PRAGMA user_version=3;
`

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-' })
}

// IndexBatch bounds attempts, including failures, while each artifact commits independently.
func (s *Store) IndexBatch(limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrUnavailable
	}
	processed := 0
	var failure error
	for range limit {
		attempted, err := s.IndexNext(context.Background(), false)
		if err != nil {
			failure = err
		}
		if !attempted {
			break
		}
		if err == nil {
			processed++
		}
	}
	return processed, failure
}

// IndexNext replaces postings, the checkpoint and the queue entry in one cancellable transaction.
func (s *Store) IndexNext(ctx context.Context, includeBulk bool) (attempted bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if _, err := s.authority.Inventory(); err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, ErrUnavailable
	}
	defer tx.Rollback()
	var id string
	var bulk bool
	err = tx.QueryRowContext(ctx, "SELECT artifact_id,bulk FROM index_queue WHERE failure='' AND (bulk=0 OR ?) ORDER BY bulk,artifact_id LIMIT 1", includeBulk).Scan(&id, &bulk)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, ErrUnavailable
	}
	defer func() {
		if err == nil {
			return
		}
		tx.Rollback()
		if ctx.Err() != nil {
			err = ctx.Err()
			return
		}
		if _, persistErr := s.db.Exec("UPDATE index_queue SET failure='indexing failed' WHERE artifact_id=?", id); persistErr != nil {
			attempted = false
		}
		err = ErrUnavailable
	}()
	if err = indexArtifact(ctx, tx, id, bulk); err != nil {
		return true, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM index_queue WHERE artifact_id=?", id); err != nil {
		return true, err
	}
	if err = tx.Commit(); err != nil {
		return true, err
	}
	return true, nil
}

func indexArtifact(ctx context.Context, tx *sql.Tx, id string, force bool) error {
	var revision, content, source string
	if err := tx.QueryRowContext(ctx, "SELECT a.head,coalesce(r.content,''),coalesce(r.source,'') FROM artifacts a LEFT JOIN revisions r ON r.id=a.head WHERE a.id=?", id).Scan(&revision, &content, &source); err != nil {
		return err
	}
	var indexed string
	err := tx.QueryRowContext(ctx, "SELECT revision_id FROM indexed WHERE artifact_id=?", id).Scan(&indexed)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if !force && err == nil && indexed == revision {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tokens WHERE artifact_id=?", id); err != nil {
		return err
	}
	a, err := loadAssociations(tx, revision)
	if err != nil {
		return err
	}
	text := content + " " + strings.Join(a.Identifiers, " ") + " " + strings.Join(a.Aliases, " ") + " " + strings.Join(a.Topics, " ")
	if source != "" {
		r, err := loadRevision(tx, id, revision)
		if err != nil {
			return err
		}
		for _, file := range r.Source.Files {
			text += " " + file.Content
		}
	}
	statement, err := tx.PrepareContext(ctx, "INSERT INTO tokens VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	defer statement.Close()
	tokens := tokenize(text)
	limited := len(tokens) > MaxIndexedTokens
	if limited {
		tokens = tokens[:MaxIndexedTokens]
	}
	for pos, token := range tokens {
		if _, err := statement.ExecContext(ctx, id, revision, token, pos); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO indexed VALUES(?,?,?) ON CONFLICT(artifact_id) DO UPDATE SET revision_id=excluded.revision_id,limited=excluded.limited", id, revision, limited)
	return err
}
