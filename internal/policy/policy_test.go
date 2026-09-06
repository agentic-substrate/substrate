package policy

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/scope"
)

func TestCheckDeniesOrgWriteForTeamAdminMembership(t *testing.T) {
	p := identity.Principal{
		ID:           uuid.Must(uuid.NewV7()),
		Trust:        identity.TrustHuman,
		TeamIDs:      []uuid.UUID{uuid.Must(uuid.NewV7())},
		Capabilities: []string{"memory:write"},
	}
	sc, err := scope.Parse("global:/org:acme")
	if err != nil {
		t.Fatal(err)
	}
	err = Check("memory.write", sc, p)
	if !errors.Is(err, ErrDeniedScope) {
		t.Fatalf("human with membership.role unused still has trust=human; org write: %v, want SUBSTRATE_DENIED_SCOPE", err)
	}
	if p.IsAdmin() {
		t.Fatal("precondition: IsAdmin must be false")
	}
}

func TestCheckAllowsHumanAdminOnOrg(t *testing.T) {
	p := identity.Principal{
		Trust:        identity.TrustHumanAdmin,
		Capabilities: []string{"memory:write"},
	}
	sc, err := scope.Parse("global:/org:acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := Check("memory.write", sc, p); err != nil {
		t.Fatalf("human_admin org write: %v", err)
	}
}

func TestCheckRequiresTeamMembershipOnProject(t *testing.T) {
	sc, err := scope.Parse("global:/org:acme/team:alpha/project:plotlens")
	if err != nil {
		t.Fatal(err)
	}
	outsider := identity.Principal{Trust: identity.TrustHuman, Capabilities: []string{"memory:write"}}
	if err := Check("memory.write", sc, outsider); !errors.Is(err, ErrDeniedScope) {
		t.Fatalf("no teams: %v, want denied", err)
	}
	member := identity.Principal{
		Trust:        identity.TrustHuman,
		TeamIDs:      []uuid.UUID{uuid.Must(uuid.NewV7())},
		Capabilities: []string{"memory:write"},
	}
	if err := Check("memory.write", sc, member); err != nil {
		t.Fatalf("member project write: %v", err)
	}
}

func TestCheckRequiresCapability(t *testing.T) {
	sc, err := scope.Parse("global:/org:acme/team:alpha/project:plotlens")
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{
		Trust:        identity.TrustHuman,
		TeamIDs:      []uuid.UUID{uuid.Must(uuid.NewV7())},
		Capabilities: []string{"instruction:propose"},
	}
	if err := Check("memory.write", sc, p); !errors.Is(err, ErrDeniedScope) {
		t.Fatalf("missing memory:write: %v, want denied", err)
	}
}
