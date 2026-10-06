package artifacts

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/agentic-substrate/substrate/internal/strictjson"
)

func validateBackupIndex(db *sql.DB) error {
	var orphaned int
	if err := db.QueryRow("SELECT count(*) FROM tokens t WHERE NOT EXISTS(SELECT 1 FROM indexed i WHERE i.artifact_id=t.artifact_id)").Scan(&orphaned); err != nil {
		return err
	}
	if orphaned != 0 {
		return errors.New("index postings have no checkpoint")
	}
	last := ""
	for {
		var id, revision string
		var limited bool
		err := db.QueryRow("SELECT artifact_id,revision_id,limited FROM indexed WHERE artifact_id>? ORDER BY artifact_id LIMIT 1", last).Scan(&id, &revision, &limited)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		last = id
		var content, source, metadata string
		if revision != "" {
			if err := db.QueryRow("SELECT r.content,r.source,coalesce(a.metadata,'{}') FROM revisions r LEFT JOIN revision_associations a ON a.revision_id=r.id WHERE r.id=? AND r.artifact_id=?", revision, id).Scan(&content, &source, &metadata); err != nil {
				return err
			}
		}
		var associations Associations
		if metadata != "" {
			if err := strictjson.Decode([]byte(metadata), &associations); err != nil {
				return err
			}
		}
		text := content + " " + strings.Join(associations.Identifiers, " ") + " " + strings.Join(associations.Aliases, " ") + " " + strings.Join(associations.Topics, " ")
		if source != "" {
			var src Source
			if err := strictjson.Decode([]byte(source), &src); err != nil {
				return err
			}
			for _, file := range src.Files {
				text += " " + file.Content
			}
		}
		want := tokenize(text)
		if limited != (len(want) > MaxIndexedTokens) {
			return errors.New("index checkpoint has an invalid token limit")
		}
		if len(want) > MaxIndexedTokens {
			want = want[:MaxIndexedTokens]
		}
		rows, err := db.Query("SELECT revision_id,token,position FROM tokens WHERE artifact_id=? ORDER BY position", id)
		if err != nil {
			return err
		}
		position := 0
		for rows.Next() {
			var actualRevision, token string
			var actualPosition int
			if err := rows.Scan(&actualRevision, &token, &actualPosition); err != nil {
				rows.Close()
				return err
			}
			if position >= len(want) || actualRevision != revision || actualPosition != position || token != want[position] {
				rows.Close()
				return errors.New("index postings do not match their captured revision checkpoint")
			}
			position++
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if position != len(want) {
			return errors.New("index postings are incomplete for their captured revision checkpoint")
		}
	}
}
