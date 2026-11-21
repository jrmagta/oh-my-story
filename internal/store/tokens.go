package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jrmagta/oh-my-story/internal/role"
)

// APIToken is a workspace API token as admins see it; the secret itself is
// shown once, when it is made.
type APIToken struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Scope       string     `json:"scope"` // read or write
	Prefix      string     `json:"prefix"`
	CreatorName *string    `json:"creatorName"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// TokenScopes maps a token's scope to the role it acts with.
var TokenScopes = map[string]role.Role{"read": role.Viewer, "write": role.Editor}

const apiTokenCols = `t.id, t.name, t.scope, t.prefix, u.display_name, t.expires_at, t.last_used_at, t.created_at`

func scanAPIToken(row pgx.Row) (APIToken, error) {
	var t APIToken
	err := row.Scan(&t.ID, &t.Name, &t.Scope, &t.Prefix, &t.CreatorName, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	return t, notFound(err)
}

// CreateAPIToken stores a token and the bot user it acts as. The bot is not a
// member of the workspace and cannot sign in.
func (s *Store) CreateAPIToken(ctx context.Context, actor, workspaceID uuid.UUID, name, scope, prefix string, hash []byte, expires *time.Time) (APIToken, error) {
	id, bot := newID(), newID()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, email, display_name, is_bot) VALUES ($1, $2, $3, true)`,
			bot, "token-"+bot.String()+"@bots.invalid", name+" (token)"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO api_tokens (id, workspace_id, bot_user_id, name, scope, prefix, token_hash, created_by, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, workspaceID, bot, name, scope, prefix, hash, actor, expires)
		return err
	})
	if err != nil {
		return APIToken{}, dbError(err)
	}
	return scanAPIToken(s.pool.QueryRow(ctx,
		`SELECT `+apiTokenCols+` FROM api_tokens t LEFT JOIN users u ON u.id = t.created_by WHERE t.id = $1`, id))
}

// APITokens lists a workspace's tokens that are not revoked, newest first.
func (s *Store) APITokens(ctx context.Context, workspaceID uuid.UUID) ([]APIToken, error) {
	return collect(ctx, s.pool, scanAPIToken,
		`SELECT `+apiTokenCols+` FROM api_tokens t LEFT JOIN users u ON u.id = t.created_by
		 WHERE t.workspace_id = $1 AND t.revoked_at IS NULL ORDER BY t.created_at DESC, t.id`, workspaceID)
}

func (s *Store) RevokeAPIToken(ctx context.Context, workspaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND workspace_id = $2 AND revoked_at IS NULL`, id, workspaceID)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	return err
}

// TokenGrant is what a live token may do: act in one workspace with a role.
type TokenGrant struct {
	WorkspaceID uuid.UUID
	Role        role.Role
}

// UserByAPIToken finds the bot user of a live (not revoked or expired)
// token, and notes the use (at most once a minute).
func (s *Store) UserByAPIToken(ctx context.Context, hash []byte) (User, TokenGrant, error) {
	var u User
	var g TokenGrant
	var scope string
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT t.id, t.workspace_id, t.scope, u.id, u.email, u.display_name
		 FROM api_tokens t JOIN users u ON u.id = t.bot_user_id
		 WHERE t.token_hash = $1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())`, hash).
		Scan(&id, &g.WorkspaceID, &scope, &u.ID, &u.Email, &u.DisplayName)
	if err != nil {
		return User{}, TokenGrant{}, notFound(err)
	}
	g.Role = TokenScopes[scope]
	if _, err := s.pool.Exec(ctx,
		`UPDATE api_tokens SET last_used_at = now() WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`,
		id); err != nil {
		return User{}, TokenGrant{}, err
	}
	return u, g, nil
}

// TokenMembership is the workspace {slug} as a token of workspaceID sees it:
// ErrNotFound for any other workspace.
func (s *Store) TokenMembership(ctx context.Context, g TokenGrant, slug string) (Membership, error) {
	var m Membership
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, slug, key_prefix, estimate_unit FROM workspaces WHERE id = $1 AND slug = $2`, g.WorkspaceID, slug).
		Scan(&m.ID, &m.Name, &m.Slug, &m.KeyPrefix, &m.EstimateUnit)
	m.Role = g.Role
	return m, notFound(err)
}
