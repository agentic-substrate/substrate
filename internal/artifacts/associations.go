package artifacts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type Associations struct {
	Identifiers []string `json:"identifiers,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Topics      []string `json:"topics,omitempty"`
	Related     []string `json:"related,omitempty"`
}

func (a Associations) validate() error {
	for _, values := range [][]string{a.Identifiers, a.Aliases, a.Topics, a.Related} {
		if len(values) > 32 {
			return errors.New("associations require at most 32 entries per field")
		}
		seen := map[string]bool{}
		for _, value := range values {
			if !validText(value) {
				return ErrInvalidText
			}
			if strings.TrimSpace(value) != value || value == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") || seen[value] {
				return errors.New("association labels must be distinct, nonempty, and at most 256 bytes")
			}
			seen[value] = true
		}
	}
	return nil
}

func saveAssociations(tx *sql.Tx, ctx authority.Context, revision string, a Associations) error {
	for _, id := range a.Related {
		if _, err := lookup(tx, ctx, id); err != nil {
			return err
		}
	}
	data, _ := json.Marshal(a)
	if _, err := tx.Exec("INSERT INTO revision_associations VALUES(?,?)", revision, string(data)); err != nil {
		return ErrUnavailable
	}
	return nil
}
func loadAssociations(tx *sql.Tx, revision string) (Associations, error) {
	var data string
	err := tx.QueryRow("SELECT metadata FROM revision_associations WHERE revision_id=?", revision).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Associations{}, nil
	}
	if err != nil {
		return Associations{}, ErrUnavailable
	}
	var a Associations
	if json.Unmarshal([]byte(data), &a) != nil {
		return Associations{}, ErrUnavailable
	}
	return a, nil
}
