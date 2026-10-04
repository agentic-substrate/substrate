package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type SearchRequest struct {
	RelatedTo string `json:"related_to,omitempty"`
	Query     string `json:"query"`
	Limit     int    `json:"limit,omitempty"`
}
type ReadRequest struct {
	ArtifactID string `json:"artifact_id,omitempty"`
	RevisionID string `json:"revision_id,omitempty"`
	Selector   string `json:"selector,omitempty"`
}
type SearchResult struct {
	ArtifactID   string       `json:"artifact_id"`
	RevisionID   string       `json:"revision_id"`
	Kind         string       `json:"kind"`
	State        string       `json:"state"`
	Qualified    string       `json:"qualified,omitempty"`
	Excerpt      string       `json:"excerpt"`
	Score        int          `json:"score"`
	Associations Associations `json:"associations"`
}
type IndexState struct {
	Limited  int `json:"limited"`
	Eligible int `json:"eligible"`
	Indexed  int `json:"indexed"`
	Pending  int `json:"pending"`
}
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Index   IndexState     `json:"index"`
}

type current struct {
	id, revision, kind string
	choice             Choice
}

func eligible(tx *sql.Tx, ctx authority.Context) ([]current, error) {
	rows, err := tx.Query("SELECT id,head,kind FROM artifacts WHERE owner_id=? AND space_id=? AND repo_id=? AND lifecycle='active' AND head<>''", ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID)
	if err != nil {
		return nil, ErrUnavailable
	}
	out := []current{}
	for rows.Next() {
		var c current
		if rows.Scan(&c.id, &c.revision, &c.kind) != nil {
			rows.Close()
			return nil, ErrUnavailable
		}
		out = append(out, c)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return nil, ErrUnavailable
	}
	all, err := selections(tx, ctx)
	if err != nil {
		return nil, err
	}
	choices := map[string]selection{}
	for _, c := range all {
		choices[c.ArtifactID] = c
	}
	filtered := out[:0]
	for _, c := range out {
		if c.kind != "memory" {
			sel, ok := choices[c.id]
			if !ok || sel.relationConflict || sel.lifecycle != "active" || sel.RevisionID == "" {
				continue
			}
			c.choice = sel.Choice
		}
		filtered = append(filtered, c)
	}
	return filtered, nil
}

func (s *Session) Read(q ReadRequest) (Delivery, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return Delivery{}, err
	}
	if !validText(q.ArtifactID, q.RevisionID, q.Selector) {
		return Delivery{}, ErrInvalidText
	}
	selectors := 0
	for _, v := range []string{q.ArtifactID, q.RevisionID, q.Selector} {
		if v != "" {
			selectors++
		}
	}
	if selectors != 1 {
		return Delivery{}, errors.New("read requires exactly one artifact ID, revision ID, or selector")
	}
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Delivery{}, ErrUnavailable
	}
	defer tx.Rollback()
	all, err := eligible(tx, ctx)
	if err != nil {
		return Delivery{}, err
	}
	selected := []current{}
	for _, c := range all {
		match := q.ArtifactID == c.id || q.RevisionID == c.revision || (q.Selector != "" && q.Selector == c.choice.Qualified)
		if q.Selector != "" && !match {
			if q.Selector == c.choice.Alias {
				if c.choice.State == "conflict" {
					return Delivery{}, ErrConflict
				}
				match = c.choice.State == "effective"
			}
		}
		if match {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return Delivery{}, authority.ErrDenied
	}
	if len(selected) > 1 {
		return Delivery{}, ErrConflict
	}
	c := selected[0]
	r, err := loadRevision(tx, c.id, c.revision)
	if err != nil {
		return Delivery{}, err
	}
	if c.kind != "memory" && (r.State != "approved" || r.Source == nil) {
		return Delivery{}, ErrUnavailable
	}
	if c.kind == "memory" {
		c.choice = Choice{ArtifactID: c.id, RevisionID: c.revision, State: r.State, Reason: "current permitted unverified observation"}
	}
	// Related IDs are disclosed only after checking each endpoint's current eligibility.
	allowed := map[string]bool{}
	for _, v := range all {
		allowed[v.id] = true
	}
	related := []string{}
	for _, id := range r.Associations.Related {
		if allowed[id] {
			related = append(related, id)
		}
	}
	r.Associations.Related = related
	return Delivery{Choice: c.choice, Revision: r, NativeActivation: "unsupported"}, nil
}

func (s *Session) Search(q SearchRequest) (SearchResponse, error) {
	ctx, err := s.authenticate()
	if err != nil {
		return SearchResponse{}, err
	}
	if !validText(q.Query, q.RelatedTo) {
		return SearchResponse{}, ErrInvalidText
	}
	if len(q.Query) > 4096 || q.Limit < 0 || q.Limit > 100 {
		return SearchResponse{}, errors.New("search requires a query up to 4096 bytes and limit up to 100")
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	terms := tokenize(q.Query)
	if len(terms) > 32 {
		return SearchResponse{}, errors.New("search requires at most 32 terms")
	}
	prefix := strings.HasSuffix(q.Query, "*") && len(terms) > 0 && utf8.RuneCountInString(terms[len(terms)-1]) >= 3
	tx, err := s.store.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SearchResponse{}, ErrUnavailable
	}
	defer tx.Rollback()
	all, err := eligible(tx, ctx)
	if err != nil {
		return SearchResponse{}, err
	}
	response := SearchResponse{Results: []SearchResult{}, Index: IndexState{Eligible: len(all)}}
	byID := map[string]current{}
	for _, c := range all {
		byID[c.id] = c
	}
	rows, err := tx.Query(`SELECT a.id,i.revision_id,coalesce(i.limited,0) FROM artifacts a LEFT JOIN indexed i ON i.artifact_id=a.id WHERE a.owner_id=? AND a.space_id=? AND a.repo_id=? AND a.lifecycle='active' AND a.head<>''`, ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID)
	if err != nil {
		return SearchResponse{}, ErrUnavailable
	}
	for rows.Next() {
		var id string
		var rev sql.NullString
		var limited bool
		if rows.Scan(&id, &rev, &limited) != nil {
			rows.Close()
			return SearchResponse{}, ErrUnavailable
		}
		if c, ok := byID[id]; ok && rev.String == c.revision {
			response.Index.Indexed++
			if limited {
				response.Index.Limited++
			}
		}
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return SearchResponse{}, ErrUnavailable
	}
	response.Index.Pending = response.Index.Eligible - response.Index.Indexed
	scores := map[string]int{}
	matches := map[string]map[int]bool{}
	positions := map[string]map[int]map[int]bool{}
	for i, term := range terms {
		clause := "t.token=?"
		pattern := term
		if prefix && i == len(terms)-1 {
			clause = "t.token>=? AND t.token<?"
		}
		args := []any{ctx.OwnerID, ctx.SpaceID, ctx.RepositoryID, pattern}
		if clause != "t.token=?" {
			args = append(args, pattern+"\U0010ffff")
		}
		rows, err := tx.Query(`SELECT t.artifact_id,t.position FROM tokens t JOIN artifacts a ON a.id=t.artifact_id AND a.head=t.revision_id WHERE a.owner_id=? AND a.space_id=? AND a.repo_id=? AND a.lifecycle='active' AND `+clause, args...)
		if err != nil {
			return SearchResponse{}, ErrUnavailable
		}
		for rows.Next() {
			var id string
			var pos int
			if rows.Scan(&id, &pos) != nil {
				rows.Close()
				return SearchResponse{}, ErrUnavailable
			}
			if _, ok := byID[id]; !ok {
				continue
			}
			if matches[id] == nil {
				matches[id] = map[int]bool{}
				positions[id] = map[int]map[int]bool{}
			}
			matches[id][i] = true
			if positions[id][i] == nil {
				positions[id][i] = map[int]bool{}
			}
			positions[id][i][pos] = true
		}
		scanErr := rows.Err()
		rows.Close()
		if scanErr != nil {
			return SearchResponse{}, ErrUnavailable
		}
	}
	for id, matched := range matches {
		score := len(matched) * 100
		for _, freq := range positions[id] {
			score += min(len(freq), 10)
		}
		for start := range positions[id][0] {
			phrase := len(terms) > 1
			for i := 1; i < len(terms); i++ {
				phrase = phrase && positions[id][i][start+i]
			}
			if phrase {
				score += 20
				break
			}
		}
		scores[id] = score
	}
	for _, c := range all {
		a, err := loadAssociations(tx, c.revision)
		if err != nil {
			return SearchResponse{}, err
		}
		exact := q.Query == c.id || q.Query == c.revision || q.Query == c.choice.Qualified || (q.Query != "" && q.Query == c.choice.Alias)
		for _, label := range append(append(append([]string{}, a.Identifiers...), a.Aliases...), a.Topics...) {
			exact = exact || strings.EqualFold(q.Query, label)
		}
		if exact {
			scores[c.id] += 1000
		}
		if len(terms) == 0 && strings.TrimSpace(q.Query) == "" {
			scores[c.id] = 0
		}
	}
	if q.RelatedTo != "" {
		origin, ok := byID[q.RelatedTo]
		if !ok {
			return SearchResponse{}, authority.ErrDenied
		}
		associations, err := loadAssociations(tx, origin.revision)
		if err != nil {
			return SearchResponse{}, err
		}
		targets := map[string]bool{}
		for _, id := range associations.Related {
			if _, ok := byID[id]; ok {
				targets[id] = true
			}
		}
		for id := range scores {
			if !targets[id] {
				delete(scores, id)
			}
		}
	}
	ids := []string{}
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > q.Limit {
		ids = ids[:q.Limit]
	}
	for _, id := range ids {
		c := byID[id]
		r, err := loadRevision(tx, c.id, c.revision)
		if err != nil {
			return SearchResponse{}, err
		}
		if c.kind != "memory" && (r.State != "approved" || r.Source == nil) {
			return SearchResponse{}, ErrUnavailable
		}
		related := []string{}
		for _, id := range r.Associations.Related {
			if _, ok := byID[id]; ok {
				related = append(related, id)
			}
		}
		r.Associations.Related = related
		excerpt := []rune(r.Content)
		if len(excerpt) > 400 {
			excerpt = excerpt[:400]
		}
		response.Results = append(response.Results, SearchResult{ArtifactID: id, RevisionID: c.revision, Kind: c.kind, State: r.State, Qualified: c.choice.Qualified, Excerpt: string(excerpt), Score: scores[id], Associations: r.Associations})
	}
	return response, nil
}
