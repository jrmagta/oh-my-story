package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ShareLink is a read-only link to a map. Its token is never stored.
type ShareLink struct {
	ID          uuid.UUID  `json:"id"`
	CreatorName string     `json:"creatorName"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

const shareCols = `l.id, coalesce(u.display_name, ''), l.expires_at, l.last_used_at, l.created_at`

func scanShare(row pgx.Row) (ShareLink, error) {
	var l ShareLink
	err := row.Scan(&l.ID, &l.CreatorName, &l.ExpiresAt, &l.LastUsedAt, &l.CreatedAt)
	return l, notFound(err)
}

// CreateShareLink stores a link's token hash; expires nil never expires.
func (s *Store) CreateShareLink(ctx context.Context, actor, workspaceID, mapID uuid.UUID, tokenHash []byte, expires *time.Time) (ShareLink, error) {
	if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
		return ShareLink{}, err
	}
	id := newID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO share_links (id, map_id, token_hash, created_by, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		id, mapID, tokenHash, actor, expires); err != nil {
		return ShareLink{}, err
	}
	return scanShare(s.pool.QueryRow(ctx,
		`SELECT `+shareCols+` FROM share_links l LEFT JOIN users u ON u.id = l.created_by WHERE l.id = $1`, id))
}

// ShareLinks lists a map's links that are not revoked, newest first; expired ones too.
func (s *Store) ShareLinks(ctx context.Context, workspaceID, mapID uuid.UUID) ([]ShareLink, error) {
	if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
		return nil, err
	}
	return collect(ctx, s.pool, scanShare,
		`SELECT `+shareCols+` FROM share_links l LEFT JOIN users u ON u.id = l.created_by
		 WHERE l.map_id = $1 AND l.revoked_at IS NULL ORDER BY l.created_at DESC, l.id`, mapID)
}

func (s *Store) RevokeShareLink(ctx context.Context, workspaceID, mapID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE share_links l SET revoked_at = now() FROM maps m
		 WHERE l.id = $3 AND l.map_id = $2 AND m.id = l.map_id AND m.workspace_id = $1 AND l.revoked_at IS NULL`,
		workspaceID, mapID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SharedMap is what a share link shows: the map read-only, without the
// people (assignees) or the conversation (comment counts).
type SharedMap struct {
	WorkspaceName string  `json:"workspaceName"`
	EstimateUnit  string  `json:"estimateUnit"`
	View          MapView `json:"view"`
}

// SharedMapByToken resolves a live, unexpired, unrevoked link to a live map
// and records that it was used. Anything else is ErrNotFound.
func (s *Store) SharedMapByToken(ctx context.Context, tokenHash []byte) (SharedMap, error) {
	var out SharedMap
	var workspaceID, mapID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`UPDATE share_links l SET last_used_at = now() FROM maps m JOIN workspaces w ON w.id = m.workspace_id
		 WHERE l.token_hash = $1 AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
		   AND m.id = l.map_id AND m.deleted_at IS NULL
		 RETURNING w.id, m.id, w.name, w.estimate_unit`, tokenHash).Scan(&workspaceID, &mapID, &out.WorkspaceName, &out.EstimateUnit)
	if err != nil {
		return out, notFound(err)
	}
	if out.View, err = s.MapView(ctx, workspaceID, mapID); err != nil {
		return out, err
	}
	for i := range out.View.Steps {
		out.View.Steps[i].AssigneeID = nil
		out.View.Steps[i].CommentCount = 0
	}
	return out, nil
}
