package artifacts

import "github.com/agentic-substrate/substrate/internal/authority"

type PublicationSource struct {
	ArtifactID string `json:"artifact_id"`
	RevisionID string `json:"revision_id"`
}
type PublicationDestination struct {
	SpaceID      string `json:"space_id"`
	RepositoryID string `json:"repo_id"`
}
type PublicationInput struct {
	OperationID      string                 `json:"operation_id"`
	ID               string                 `json:"id,omitempty"`
	ExpectedRevision string                 `json:"expected_revision,omitempty"`
	Content          string                 `json:"content"`
	Sources          []PublicationSource    `json:"sources"`
	Destination      PublicationDestination `json:"destination"`
}
type Publication struct {
	ID          string                 `json:"id"`
	RevisionID  string                 `json:"revision_id"`
	State       string                 `json:"state"`
	Content     string                 `json:"content"`
	Sources     []PublicationSource    `json:"sources"`
	Destination PublicationDestination `json:"destination"`
}
type PublicationPolicy struct {
	Scope   string `json:"scope"`
	ID      string `json:"id"`
	Action  string `json:"action"`
	Allowed bool   `json:"allowed"`
	Epoch   int64  `json:"epoch"`
}
type PublicationSourceSnapshot struct {
	ArtifactID string   `json:"artifact_id"`
	Kind       string   `json:"kind"`
	Revision   Revision `json:"revision"`
	Choice     Choice   `json:"choice"`
}
type PublicationReview struct {
	Proposal           Publication                 `json:"proposal"`
	SourceContext      authority.Context           `json:"source_context"`
	DestinationContext authority.Context           `json:"destination_context"`
	Sources            []PublicationSourceSnapshot `json:"sources"`
	Policies           []PublicationPolicy         `json:"policies"`
	Audience           string                      `json:"audience"`
	Placement          string                      `json:"placement"`
	Provenance         string                      `json:"provenance"`
	Dependencies       []SourceFile                `json:"dependencies"`
	Snapshot           string                      `json:"snapshot"`
}
type PublicationApproval struct {
	OperationID string `json:"operation_id"`
	RevisionID  string `json:"revision_id"`
	Snapshot    string `json:"snapshot"`
}
