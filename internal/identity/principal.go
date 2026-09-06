// Package identity loads principals from bearer tokens and applies the RLS
// session settings those principals authorize.
package identity

import (
	"time"

	"github.com/google/uuid"
)

// Trust is principal.trust. IsAdmin is true only for HumanAdmin (EDD R25).
type Trust string

// Trust values match the trust_level enum.
const (
	TrustHumanAdmin       Trust = "human_admin"
	TrustHuman            Trust = "human"
	TrustAgentInteractive Trust = "agent_interactive"
	TrustAgentAutonomous  Trust = "agent_autonomous"
)

// Kind is principal.kind.
type Kind string

// Kind values match the principal_kind enum.
const (
	KindUser  Kind = "user"
	KindAgent Kind = "agent"
)

// AgentTokenTTL is the hard lifetime of a token minted for an agent (EDD R3).
const AgentTokenTTL = 24 * time.Hour

// Principal is the authenticated actor for one request.
type Principal struct {
	ID                uuid.UUID
	Kind              Kind
	Trust             Trust
	DisplayName       string
	TeamIDs           []uuid.UUID
	OrgID             uuid.UUID
	GrantedProjectIDs []uuid.UUID
	Capabilities      []string
	Disabled          bool
}

// IsAdmin reports org/global admin. membership.role='admin' never sets this.
func (p Principal) IsAdmin() bool {
	return p.Trust == TrustHumanAdmin
}
