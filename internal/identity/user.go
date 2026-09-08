package identity

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CreateUserInput is the operator-facing request behind `substrate admin
// create-user`. Org and Team are required: a user principal with no membership
// can authenticate but read nothing, which looks like a server bug.
type CreateUserInput struct {
	DisplayName string
	Org         string
	Team        string
	Machine     string
	// Admin mints the principal at human_admin trust and makes it a team
	// admin. The default is human at member role; IsAdmin is true only for
	// human_admin (EDD R25).
	Admin bool
	// Scopes are the token's capabilities, e.g. memory:write.
	Scopes []string
}

// CreateUserResult is shown once. The database holds only the token's hash.
// OrgID and TeamID are the ids of the rows the principal is actually attached
// to, which are the pre-existing rows when --org or --team names one that is
// already there -- not the ids CreateUser generated and then discarded.
type CreateUserResult struct {
	PrincipalID uuid.UUID
	OrgID       uuid.UUID
	TeamID      uuid.UUID
	Token       string
}

// CreateUser creates the org, team, user principal, membership and API token
// that bootstrap an installation, and is the only path in the codebase that
// produces a `user` principal -- Mint refuses For != "agent". It touches five
// tables and must therefore be called inside a transaction: every caller
// passes a pgx.Tx so a failure partway through leaves zero rows rather than an
// org with no one in it. The minted token has a NULL expires_at, because
// lookup.go TTL-caps agent tokens only; a user token does not expire.
func CreateUser(ctx context.Context, tx DBTX, in CreateUserInput) (CreateUserResult, error) {
	if in.DisplayName == "" {
		return CreateUserResult{}, fmt.Errorf("admin create-user: --name is required")
	}
	if in.Org == "" || in.Team == "" {
		return CreateUserResult{}, fmt.Errorf("admin create-user: --org and --team are required")
	}
	if in.Machine == "" {
		return CreateUserResult{}, fmt.Errorf("admin create-user: --machine is required")
	}
	trust := TrustHuman
	// The membership role follows --admin rather than being hardcoded: a
	// bootstrap user created without --admin used to land as a team admin,
	// which is an escalation the flag help did not describe.
	role := "member"
	if in.Admin {
		trust = TrustHumanAdmin
		role = "admin"
	}
	ids := make([]uuid.UUID, 4)
	for i := range ids {
		id, err := uuid.NewV7()
		if err != nil {
			return CreateUserResult{}, fmt.Errorf("admin create-user: %w", err)
		}
		ids[i] = id
	}
	orgID, teamID, princID, tokID := ids[0], ids[1], ids[2], ids[3]
	raw, err := NewToken()
	if err != nil {
		return CreateUserResult{}, err
	}
	scopes := in.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	// ON CONFLICT DO UPDATE, not DO NOTHING: DO NOTHING returns no row when
	// the org already exists, and the result would then carry a generated id
	// that names no row anywhere.
	if err := tx.QueryRow(ctx, `INSERT INTO org (id, name) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, orgID, in.Org).Scan(&orgID); err != nil {
		return CreateUserResult{}, fmt.Errorf("admin create-user: org: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO team (id, org_id, name) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, teamID, orgID, in.Team).Scan(&teamID); err != nil {
		return CreateUserResult{}, fmt.Errorf("admin create-user: team: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO principal (id, kind, display_name, trust)
		VALUES ($1, 'user', $2, $3)`, princID, in.DisplayName, trust); err != nil {
		return CreateUserResult{}, fmt.Errorf("admin create-user: principal: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO membership (principal_id, team_id, role)
		VALUES ($1, $2, $3)`, princID, teamID, role); err != nil {
		return CreateUserResult{}, fmt.Errorf("admin create-user: membership: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes)
		VALUES ($1, $2, $3, $4, $5)`, tokID, princID, in.Machine, HashToken(raw), scopes); err != nil {
		return CreateUserResult{}, fmt.Errorf("admin create-user: token: %w", err)
	}
	return CreateUserResult{PrincipalID: princID, OrgID: orgID, TeamID: teamID, Token: EncodeToken(raw)}, nil
}
