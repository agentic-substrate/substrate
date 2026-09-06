package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the subset of pgx used for token lookup and mint. *pgxpool.Pool and
// pgx.Tx both satisfy it.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Lookup authenticates a bearer token and loads principal, memberships, and
// granted projects. Revoked, expired, disabled, and >24h agent tokens fail
// before any content query runs.
func Lookup(ctx context.Context, db DBTX, token string) (*Principal, error) {
	raw, err := DecodeToken(token)
	if err != nil {
		return nil, ErrUnauthorized
	}
	var (
		id         uuid.UUID
		kind       string
		trust      string
		display    string
		disabledAt *time.Time
		expiresAt  *time.Time
		revokedAt  *time.Time
		createdAt  time.Time
		scopes     []string
	)
	err = db.QueryRow(ctx, `
		SELECT p.id, p.kind::text, p.trust::text, p.display_name, p.disabled_at,
		       t.expires_at, t.revoked_at, t.created_at, t.scopes
		FROM api_token t
		JOIN principal p ON p.id = t.principal_id
		WHERE t.token_hash = $1`, HashToken(raw)).Scan(
		&id, &kind, &trust, &display, &disabledAt,
		&expiresAt, &revokedAt, &createdAt, &scopes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("token lookup: %w", err)
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, ErrUnauthorized
	}
	if kind == string(KindAgent) && time.Since(createdAt) > AgentTokenTTL {
		return nil, ErrUnauthorized
	}

	p := &Principal{
		ID:           id,
		Kind:         Kind(kind),
		Trust:        Trust(trust),
		DisplayName:  display,
		Capabilities: scopes,
		Disabled:     disabledAt != nil,
	}
	if revokedAt != nil || p.Disabled {
		return nil, ErrUnauthorized
	}
	if err := loadMemberships(ctx, db, p); err != nil {
		return nil, err
	}
	return p, nil
}

func loadMemberships(ctx context.Context, db DBTX, p *Principal) error {
	rows, err := db.Query(ctx, `SELECT team_id FROM membership WHERE principal_id = $1 ORDER BY team_id`, p.ID)
	if err != nil {
		return fmt.Errorf("memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tid uuid.UUID
		if err := rows.Scan(&tid); err != nil {
			return fmt.Errorf("memberships: %w", err)
		}
		p.TeamIDs = append(p.TeamIDs, tid)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("memberships: %w", err)
	}
	if len(p.TeamIDs) == 0 {
		return nil
	}
	arr := formatUUIDArray(p.TeamIDs)
	_ = db.QueryRow(ctx, `SELECT org_id FROM team WHERE id = ANY($1::uuid[]) LIMIT 1`, arr).Scan(&p.OrgID)

	grows, err := db.Query(ctx, `SELECT DISTINCT project_scope_id FROM project_grant WHERE team_id = ANY($1::uuid[]) ORDER BY 1`, arr)
	if err != nil {
		return fmt.Errorf("project grants: %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var gid uuid.UUID
		if err := grows.Scan(&gid); err != nil {
			return fmt.Errorf("project grants: %w", err)
		}
		p.GrantedProjectIDs = append(p.GrantedProjectIDs, gid)
	}
	if err := grows.Err(); err != nil {
		return fmt.Errorf("project grants: %w", err)
	}
	return nil
}

func formatUUIDArray(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return "{}"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}
