package artifacts

import (
	"database/sql"
	"errors"

	"github.com/agentic-substrate/substrate/internal/authority"
)

var ErrUnavailable = errors.New("artifact storage unavailable: repair the private state directory or use a compatible schema")
var ErrOperation = errors.New("operation ID already used with different content")
var ErrConflict = errors.New("expected revision no longer current")
var ErrInvalidText = errors.New("artifact text must be valid UTF-8")

type Store struct {
	db        *sql.DB
	authority *authority.Store
}
type Session struct {
	store           *Store
	token, checkout string
}
type SourceFile struct {
	Path    string `json:"path"`
	Blob    string `json:"blob,omitempty"`
	Content string `json:"content,omitempty"`
}
type Source struct {
	Files  []SourceFile `json:"files,omitempty"`
	Commit string       `json:"commit"`
	Path   string       `json:"path"`
	Blob   string       `json:"blob"`
}
type Contribution struct {
	Associations     Associations `json:"associations,omitzero"`
	OperationID      string       `json:"operation_id"`
	ArtifactID       string       `json:"artifact_id,omitempty"`
	ExpectedRevision string       `json:"expected_revision,omitempty"`
	SpaceID          string       `json:"space_id,omitempty"`
	RepositoryID     string       `json:"repo_id,omitempty"`
	Kind             string       `json:"kind"`
	Content          string       `json:"content"`
	Provenance       string       `json:"provenance"`
	Source           *Source      `json:"source,omitempty"`
}
type Receipt struct {
	OperationID string `json:"operation_id"`
	ArtifactID  string `json:"artifact_id"`
	RevisionID  string `json:"revision_id"`
	State       string `json:"state"`
}
type Revision struct {
	Associations Associations `json:"associations"`
	ID           string       `json:"id"`
	Base         string       `json:"base"`
	Content      string       `json:"content"`
	Provenance   string       `json:"provenance"`
	AuthorID     string       `json:"author_id"`
	State        string       `json:"state"`
	Verification string       `json:"verification"`
	Source       *Source      `json:"source,omitempty"`
}
type Artifact struct {
	ID           string     `json:"id"`
	SpaceID      string     `json:"space_id"`
	RepositoryID string     `json:"repo_id"`
	Kind         string     `json:"kind"`
	Lifecycle    string     `json:"lifecycle"`
	HeadRevision string     `json:"head_revision"`
	Revisions    []Revision `json:"revisions"`
}
type Work struct {
	OperationID string `json:"operation_id"`
	ArtifactID  string `json:"artifact_id"`
	RevisionID  string `json:"revision_id"`
	Action      string `json:"action"`
}
