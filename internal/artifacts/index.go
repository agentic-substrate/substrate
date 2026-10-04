package artifacts

import (
	"context"
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

// IndexBatch commits derived postings and the checkpoint together; pending receipts remain intact.
func (s *Store) IndexBatch(limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrUnavailable
	}
	if _, err := s.authority.Inventory(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT artifact_id FROM index_queue ORDER BY artifact_id LIMIT ?", limit)
	if err != nil {
		return 0, ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return 0, ErrUnavailable
		}
		ids = append(ids, id)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return 0, ErrUnavailable
	}
	for _, id := range ids {
		var revision, content, source string
		err := tx.QueryRow("SELECT a.head,coalesce(r.content,''),coalesce(r.source,'') FROM artifacts a LEFT JOIN revisions r ON r.id=a.head WHERE a.id=?", id).Scan(&revision, &content, &source)
		if err != nil {
			return 0, ErrUnavailable
		}
		if _, err = tx.Exec("DELETE FROM tokens WHERE artifact_id=?", id); err != nil {
			return 0, ErrUnavailable
		}
		a, err := loadAssociations(tx, revision)
		if err != nil {
			return 0, err
		}
		text := content + " " + strings.Join(a.Identifiers, " ") + " " + strings.Join(a.Aliases, " ") + " " + strings.Join(a.Topics, " ")
		// Dependencies are explicit parts of the approved snapshot, never followed from source text.
		if source != "" {
			r, err := loadRevision(tx, id, revision)
			if err != nil {
				return 0, err
			}
			for _, file := range r.Source.Files {
				text += " " + file.Content
			}
		}
		statement, err := tx.Prepare("INSERT INTO tokens VALUES(?,?,?,?)")
		if err != nil {
			return 0, ErrUnavailable
		}
		tokens := tokenize(text)
		limited := len(tokens) > MaxIndexedTokens
		if limited {
			tokens = tokens[:MaxIndexedTokens]
		}
		for pos, token := range tokens {
			if _, err := statement.Exec(id, revision, token, pos); err != nil {
				statement.Close()
				return 0, ErrUnavailable
			}
		}
		statement.Close()
		if _, err := tx.Exec("INSERT INTO indexed VALUES(?,?,?) ON CONFLICT(artifact_id) DO UPDATE SET revision_id=excluded.revision_id, limited=excluded.limited", id, revision, limited); err != nil {
			return 0, ErrUnavailable
		}
		if _, err := tx.Exec("DELETE FROM index_queue WHERE artifact_id=?", id); err != nil {
			return 0, ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return 0, ErrUnavailable
	}
	return len(ids), nil
}
