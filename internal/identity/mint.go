package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MintInput is the operator-facing mint request.
type MintInput struct {
	For         string
	Parent      string
	Machine     string
	Scopes      []string
	DisplayName string
	Trust       Trust
}

// MintResult is shown once. Token is the wire form; the database holds only the hash.
type MintResult struct {
	PrincipalID uuid.UUID
	Token       string
	ExpiresAt   time.Time
}

// Mint creates an agent principal bound to a user, copies the parent's
// memberships, and stores only the SHA-256 of a 32-byte token (EDD §4.3).
func Mint(ctx context.Context, db DBTX, in MintInput) (MintResult, error) {
	if in.For != "agent" {
		return MintResult{}, fmt.Errorf("token mint: --for must be agent")
	}
	if in.Machine == "" {
		return MintResult{}, fmt.Errorf("token mint: --machine is required")
	}
	parent, err := uuid.Parse(in.Parent)
	if err != nil {
		return MintResult{}, fmt.Errorf("token mint: --parent: %w", err)
	}
	var parentKind string
	err = db.QueryRow(ctx, `SELECT kind::text FROM principal WHERE id = $1 AND disabled_at IS NULL`, parent).Scan(&parentKind)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MintResult{}, fmt.Errorf("token mint: parent principal not found")
		}
		return MintResult{}, fmt.Errorf("token mint: parent: %w", err)
	}
	if parentKind != string(KindUser) {
		return MintResult{}, fmt.Errorf("token mint: parent must be a user")
	}
	trust := in.Trust
	if trust == "" {
		trust = TrustAgentInteractive
	}
	name := in.DisplayName
	if name == "" {
		name = "agent"
	}
	id, err := uuid.NewV7()
	if err != nil {
		return MintResult{}, fmt.Errorf("token mint: %w", err)
	}
	tokID, err := uuid.NewV7()
	if err != nil {
		return MintResult{}, fmt.Errorf("token mint: %w", err)
	}
	raw, err := NewToken()
	if err != nil {
		return MintResult{}, err
	}
	expires := time.Now().Add(AgentTokenTTL)
	if _, err := db.Exec(ctx, `INSERT INTO principal (id, kind, display_name, trust, minted_by)
		VALUES ($1, 'agent', $2, $3, $4)`, id, name, trust, parent); err != nil {
		return MintResult{}, fmt.Errorf("token mint: principal: %w", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO membership (principal_id, team_id, role)
		SELECT $1, team_id, role FROM membership WHERE principal_id = $2`, id, parent); err != nil {
		return MintResult{}, fmt.Errorf("token mint: membership: %w", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, tokID, id, in.Machine, HashToken(raw), in.Scopes, expires); err != nil {
		return MintResult{}, fmt.Errorf("token mint: token: %w", err)
	}
	return MintResult{PrincipalID: id, Token: EncodeToken(raw), ExpiresAt: expires}, nil
}

// Revoke marks a token revoked by its wire form. Unknown tokens are unauthorized.
func Revoke(ctx context.Context, db DBTX, token string) error {
	raw, err := DecodeToken(token)
	if err != nil {
		return ErrUnauthorized
	}
	tag, err := db.Exec(ctx, `UPDATE api_token SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, HashToken(raw))
	if err != nil {
		return fmt.Errorf("token revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUnauthorized
	}
	return nil
}

// ApplySession SET LOCALs the RLS GUCs for this transaction. Names must match
// the SQL policies (`substrate.*`); a mismatch fails closed with empty reads.
func ApplySession(ctx context.Context, tx pgx.Tx, p *Principal) error {
	if p == nil {
		return nil
	}
	admin := "false"
	if p.IsAdmin() {
		admin = "true"
	}
	org := ""
	if p.OrgID != uuid.Nil {
		org = p.OrgID.String()
	}
	sets := [][2]string{
		{"substrate.actor_id", p.ID.String()},
		{"substrate.team_ids", formatUUIDArray(p.TeamIDs)},
		{"substrate.org_id", org},
		{"substrate.granted_project_ids", formatUUIDArray(p.GrantedProjectIDs)},
		{"substrate.is_admin", admin},
		{"substrate.request_id", RequestIDFrom(ctx)},
	}
	for _, kv := range sets {
		if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, kv[0], kv[1]); err != nil {
			return fmt.Errorf("session setting %s: %w", kv[0], err)
		}
	}
	return nil
}
